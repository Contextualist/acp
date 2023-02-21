import { serve, type ConnInfo } from "https://deno.land/std@0.171.0/http/server.ts";


interface ClientInfo {
  priAddr: string,
  chanName: string,
  isAuxPort: boolean,
}

interface AddrPair {
  pubAddr: string,
  priAddr: string,
}

interface ReplyInfo {
  peerAddrs: AddrPair[],
}

const auxPort = new Map<string, AddrPair[]>()

async function handleExchangeV2(req: Request, connInfo: ConnInfo): Promise<Response> {
  if (req.method != "POST")
    return new Response("Invalid method", { status: 405 })
  if (req.headers.get("Content-Type") != "application/octet-stream")
    return new Response("Invalid content type", { status: 415 })

  const pubAddr = joinHostPort(connInfo.remoteAddr)
  const conn = req.body!.getReader({ mode: "byob" })
  const { priAddr, chanName, isAuxPort = false }: ClientInfo = JSON.parse(
    new TextDecoder().decode(await receivePacket(conn))
  )
  if (isAuxPort) {
    if (!auxPort.has(chanName)) auxPort.set(chanName, [])
    auxPort.get(chanName)!.push({ pubAddr, priAddr })
    return new Response("")
  }
  const otherAP = auxPort.get(chanName) || []
  const reply: ReplyInfo = {
    peerAddrs: [{ pubAddr, priAddr }, ...otherAP]
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

  const pubAddr = joinHostPort(connInfo.remoteAddr)
  const conn = req.body!.getReader({ mode: "byob" })
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


const inbox = new Map<string, {xa: string, xb_resolve: (xb: string) => void}>()

async function exchange(name: string, x0: string, conn: ReadableStreamBYOBReader): Promise<string> {
  if (inbox.has(name)) { // the other party has set up an in-memory exchange
    const { xa: x1, xb_resolve: x0_resolve } = inbox.get(name)!
    x0_resolve(x0)
    inbox.delete(name)
    return x1
  }

  const [x1, source] = await Promise.any([
    // attempt to set up an in-memory exchange
    new Promise((resolve) => {
      inbox.set(name, { xa: x0, xb_resolve: resolve })
    }).then((x) => [x, "in-memory"]),
    // attempt to do cross-regional exchange
    exchangeViaBroadcastChannel(name, x0).then((x) => [x, "broadcast"]),
    // if client closes early
    clientClosed(conn).then(() => ["", "cancel"]),
  ])
  if (source != "in-memory") { // cancel in-memory exchange
    inbox.delete(name)
  }
  if (source != "broadcast") {
    channels.get(name)?.close()
    channels.delete(name)
  }
  return x1
}


const channels = new Map<string, BroadcastChannel>()

async function exchangeViaBroadcastChannel(name: string, x0: string): Promise<string> {
  const channel = new BroadcastChannel(name)
  channels.set(name, channel)
  channel.postMessage(x0) // if the other party has already subscribed
  const x1 = await (new Promise((resolve) => {
    channel.onmessage = (event: MessageEvent) => resolve(event.data)
  }))
  channel.postMessage(x0) // if the other party subscribes after the first post
  return x1
}


async function clientClosed(conn: ReadableStreamBYOBReader): Promise<void> {
  await conn.read(new Uint8Array(1))
  //console.log("client early close")
}


function joinHostPort(addr: Deno.NetAddr): string {
  if (addr.hostname.includes(":"))
    return `[${addr.hostname}]:${addr.port}`
  return `${addr.hostname}:${addr.port}`
}


async function receivePacket(conn: ReadableStreamBYOBReader): Promise<Uint8Array> {
  let buf = (await conn.read(new Uint8Array(2))).value
  const lenCap = 1e3
  const plen = (buf[0]<<8) | buf[1] // uint16, BE
  if (plen == 0 || plen > lenCap) {
    console.error(`received suspicious packet header declearing len=${plen}`)
    throw new Deno.errors.InvalidData
  }
  buf = (await conn.read(new Uint8Array(plen))).value
  return buf
}


function marshallPacket(data: Uint8Array): Uint8Array {
  const l = data.length
  let p = new Uint8Array(2 + l)
  p.set(new Uint8Array([(l>>8)&0xff, l&0xff])) // uint16, BE
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


serve(handler)

