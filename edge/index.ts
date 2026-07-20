type ConnInfo = Deno.ServeHandlerInfo

interface ClientInfo {
  priAddr: string,
  chanName: string,
  strategy?: string[],
  nPlan?: number,
  tsAddr?: string,
  tsCap?: number,
}

interface AddrPair {
  pubAddr: string,
  priAddr: string,
}

interface ReplyInfo {
  peerAddrs: AddrPair[],
  strategy?: string[],
  peerNPlan?: number,
  tsAddr?: string,
  tsCap?: number,
}


async function handleExchangeV2(req: Request, connInfo: ConnInfo): Promise<Response> {
  if (req.method != "POST")
    return new Response("Invalid method", { status: 405 })
  if (req.headers.get("Content-Type") != "application/octet-stream")
    return new Response("Invalid content type", { status: 415 })

  const pubAddr = joinHostPort(connInfo.remoteAddr as Deno.NetAddr)
  const conn = new PacketReader(req.body!)
  const { priAddr, chanName, nPlan = 1, ...otherInfo }: ClientInfo = JSON.parse(
    new TextDecoder().decode(await receivePacket(conn))
  )
  const reply: ReplyInfo = {
    peerAddrs: [{ pubAddr, priAddr }],
    peerNPlan: nPlan,
    ...otherInfo,
  }
  const x0 = JSON.stringify(reply)
  //console.log(`accepted from ${x0}`)

  const x1 = await exchange(chanName, x0, conn)
  if (x1 == "")
    return new Response("")
  //console.log(`exchanged, got ${x1}`)

  const msg = marshallPacket(new TextEncoder().encode(x1))
  return new Response(msg)
}


// (priAddr0|chanName) -> pubAddr1|priAddr1
async function handleExchangeV1(req: Request, connInfo: ConnInfo): Promise<Response> {
  if (req.method != "POST")
    return new Response("Invalid method", { status: 405 })
  if (req.headers.get("Content-Type") != "application/octet-stream")
    return new Response("Invalid content type", { status: 415 })

  const pubAddr = joinHostPort(connInfo.remoteAddr as Deno.NetAddr)
  const conn = new PacketReader(req.body!)
  const [priAddr, chanName] = new TextDecoder().decode(
    await receivePacket(conn)
  ).split('|')
  const x0 = `${pubAddr}|${priAddr}`
  //console.log(`accepted from ${x0}`)

  const x1 = await exchange(chanName, x0, conn)
  if (x1 == "")
    return new Response("")
  //console.log(`exchanged, got ${x1}`)

  const msg = marshallPacket(new TextEncoder().encode(x1))
  return new Response(msg)
}


type ResolveStr = (x: string) => void
const inbox = new Map<string, { xa: string, xb_resolve: ResolveStr }>()

async function exchange(name: string, x0: string, conn: PacketReader): Promise<string> {
  if (inbox.has(name)) { // the other party has set up an in-memory exchange
    const { xa: x1, xb_resolve: x0_resolve } = inbox.get(name)!
    x0_resolve(x0)
    inbox.delete(name)
    return x1
  }

  const abort = new AbortController()
  const [x1, source] = await Promise.any([
    // attempt to set up an in-memory exchange
    new Promise((resolve: ResolveStr) => {
      inbox.set(name, { xa: x0, xb_resolve: resolve })
    }).then((x) => [x, "in-memory"]),
    // attempt to do cross-regional exchange
    exchangeViaKV(name, x0, abort.signal).then((x) => [x, "kv"]),
    // if client closes early
    clientClosed(conn).then(() => ["", "cancel"]),
  ])
  if (source != "in-memory") { // cancel in-memory exchange
    inbox.delete(name)
  }
  if (source != "kv") { // cancel cross-regional exchange
    abort.abort()
  }
  return x1
}


// Cross-regional exchange via Deno KV + kv.watch().
// Two peers sharing `name` meet at a pair of KV entries:
//   slot "a" (host) claimed atomically by whoever arrives first
//   slot "b" (guest) written by the second peer as its reply
// The host watches slot "b" for the reply; the guest reads slot "a" directly.
// Since a channel name is reused across rounds, every entry carries a TTL as
// a crash backstop.
const kv = await Deno.openKv()
const EXCHANGE_TTL_MS = 30_000 // entry lifetime; refreshed while a host waits
const HEARTBEAT_MS = 10_000

async function exchangeViaKV(name: string, x0: string, signal: AbortSignal): Promise<string> {
  const keyA: Deno.KvKey = ["exchange", name, "a"]
  const keyB: Deno.KvKey = ["exchange", name, "b"]

  const claim = await kv.atomic()
    .check({ key: keyA, versionstamp: null })
    .set(keyA, x0, { expireIn: EXCHANGE_TTL_MS })
    .commit()

  if (!claim.ok) {
    // A peer already hosts this exchange
    const host = await kv.get<string>(keyA, { consistency: "strong" })
    if (host.value === null) return "" // host vanished (expired/cancelled)
    await kv.set(keyB, x0, { expireIn: EXCHANGE_TTL_MS })
    return host.value
  }

  // We host this exchange
  const claimVs = claim.versionstamp
  let settled = false
  const heartbeat = setInterval(() => {
    if (settled) return
    kv.set(keyA, x0, { expireIn: EXCHANGE_TTL_MS }).catch(() => { })
  }, HEARTBEAT_MS)
  const reader = kv.watch<[string]>([keyB]).getReader()
  const onAbort = () => reader.cancel()
  signal.addEventListener("abort", onAbort, { once: true })
  try {
    while (true) {
      const { value, done } = await reader.read()
      if (done) return "" // cancelled via abort
      const reply = value[0]
      // Only accept a reply newer than our claim so a leftover from a previous
      // round is ignored. versionstamps are globally monotonic.
      if (reply.versionstamp !== null && reply.versionstamp > claimVs)
        return reply.value!
    }
  } finally {
    settled = true
    clearInterval(heartbeat)
    signal.removeEventListener("abort", onAbort)
    reader.cancel().catch(() => { })
    // The host finishes last, so tear down both slots.
    kv.atomic().delete(keyA).delete(keyB).commit().catch(() => { })
  }
}


async function clientClosed(conn: PacketReader): Promise<void> {
  await conn.waitClosed()
  //console.log("client early close")
}


function joinHostPort(addr: Deno.NetAddr): string {
  if (addr.hostname.includes(":"))
    return `[${addr.hostname}]:${addr.port}`
  return `${addr.hostname}:${addr.port}`
}


class PacketReader {
  private reader: ReadableStreamDefaultReader<Uint8Array>
  private buf: Uint8Array = new Uint8Array(0)

  constructor(body: ReadableStream<Uint8Array>) {
    this.reader = body.getReader()
  }

  async readN(n: number): Promise<Uint8Array> {
    while (this.buf.length < n) {
      const { value, done } = await this.reader.read()
      if (done)
        throw new Deno.errors.UnexpectedEof
      const next = new Uint8Array(this.buf.length + value.length)
      next.set(this.buf)
      next.set(value, this.buf.length)
      this.buf = next
    }
    const out = this.buf.slice(0, n)
    this.buf = this.buf.slice(n)
    return out
  }

  async waitClosed(): Promise<void> {
    if (this.buf.length > 0)
      return
    try {
      await this.reader.read()
    } catch {
      // the request may be torn down after the exchange completes
    }
  }
}


async function receivePacket(conn: PacketReader): Promise<Uint8Array> {
  const header = await conn.readN(2)
  const lenCap = 1e3
  const plen = (header[0] << 8) | header[1] // uint16, BE
  if (plen == 0 || plen > lenCap) {
    console.error(`received suspicious packet header declearing len=${plen}`)
    throw new Deno.errors.InvalidData
  }
  return await conn.readN(plen)
}


function marshallPacket(data: Uint8Array): Uint8Array<ArrayBuffer> {
  const l = data.length
  const p = new Uint8Array(2 + l)
  p.set([(l >> 8) & 0xff, l & 0xff]) // uint16, BE
  p.set(data, 2)
  return p
}


async function handler(req: Request, connInfo: ConnInfo): Promise<Response> {
  const url = new URL(req.url)
  switch (url.pathname) {
    case "/get":
      return new Response(
        `
          curl -fsSL "https://bina.egoist.dev/contextualist/acp${url.search}" | sh
          if [ $# -eq 0 ]; then acp --setup; else acp "$@"; fi
        `,
        { headers: { "Content-Type": "text/plain; charset=utf-8" } }
      )
    case "/v2/exchange":
      return await handleExchangeV2(req, connInfo)
    case "/exchange":
      return await handleExchangeV1(req, connInfo)
    default:
      return new Response("Not found", { status: 404 })
  }
}


Deno.serve(handler)

