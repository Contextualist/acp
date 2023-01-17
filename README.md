# acp

![demo.gif](media/demo.gif)

Highlights (aka "Why making another file-transfer tool?"):

- Designed for personal use; no need to copy-paste a token / code for each transfer
- Rendezvous service runs distributively on [serverless edge function](https://deno.com/deploy/docs),
  a robust solution with low latency worldwide. ([How does this work?](docs/mechanism.md))

Other features:

- End-to-end encryption (ChaCha20-Poly1305)
- P2P connection: LAN or WAN, with NAT transversal
- Compression (gzip)
- Cross platform: Linux, macOS, Windows
- Support transfering multiple files and directories

See also [comparison table with similar tools](#similar-projects).


## Get started

### Linux, macOS

On any of your machine, run

```bash
curl -fsS https://acp.deno.dev/get | sh
```

It sets up the current machine by downloading an executable and generating an identity.
By default the install path is `/usr/local/bin`; you can change it by `curl -fsS 'https://acp.deno.dev/get?dir=/path/to/bin' | sh`
At the end, it prints out the command for setting up your other machines.
You can run `acp --setup` any time you want to see the command.

### Windows

Currently there is no installation script for PowerShell (PR welcomes :)
You can download the released executable and put it on your `Path`.
Then run `acp --setup` to generate an identity.


## Usage

```bash
# sender
acp path/to/files

# receiver
acp # for receiving to pwd or
acp -d path/to/dest
```

You can run the sender and receiver in arbitrary order.
Whenever both sides are up and running, they will attempt to establish a P2P connection.
If you see messages such as `rendezvous timeout`, at least one side is behind a firewall or a strict NAT that prohibits P2P connection.

For advanced configuration and self-hosting (it's free & takes only 5 minutes!), check out [the docs here](docs/advanced.md).


## Similar projects

|                                                              | [trzsz](https://github.com/trzsz/trzsz) | scp  | **acp** | [pcp](https://github.com/dennis-tra/pcp) | [croc](https://github.com/schollz/croc) |
| ------------------------------------------------------------ | --------------------------------------- | ---- | ------- | ---------------------------------------- | --------------------------------------- |
| can share files to other people /<br/>receiver needs to enter a token |                                         |      |         | O                                        | O                                       |
| LAN                                                          | O                                       | O    | O       | O                                        | O                                       |
| WAN (local ↔︎ remote)                                         | O                                       | O    | O       | P                                        | O                                       |
| WAN (remote ↔︎ remote)                                        |                                         | P    | O       | P                                        | O                                       |
| relay                                                        |                                         |      |         | P                                        | O                                       |
| p2p                                                          |                                         |      | O       | O                                        | O                                       |
| distributive                                                 |                                         |      | O       | O                                        |                                         |

O: supported; P: partial support or limited usablity; (void): not supported or not relevant

Don't judge a tool based on its apparent set of features.
This table only lists a few features, intending to differentiate the target scenarios of these tools.


## Acknowledgement

Apart from the dependencies listed in [`go.mod`](go.mod), this project is also built upon 

- [**Deno Deploy**](https://deno.com/deploy) exposes low-level connection infomation and provides a fantastic `BroadcastChannel` API that makes "serverless" TCP hole-punching possible
- [**mholt/archiver**](https://github.com/mholt/archiver): tar/untar implementation
- [**libp2p/go-reuseport**](https://github.com/libp2p/go-reuseport): address reuse for TCP hole-punching
- [**egoist/bina**](https://github.com/egoist/bina): installation script
