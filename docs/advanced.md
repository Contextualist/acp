
## Advanced options

`acp` stores config at `$HOME/.config/acp/config.json` (`%APPDATA%\acp\config.json` for Windows).
After changing config on one device, run `acp --setup` to get the command for updating configs on other devices.

List of configurable options:

- `server` (default: `"https://acp.deno.dev"`): Endpoint for coordinating rendezvous
- `ipv6` (default: `false`): Establish P2P connection using IPv6 instead of IPv4.
  Note that both ends of a connection need to use the same IP protocol.
- `ports` (default: `[0]`): Local port(s) binding for connection rendezvous.
  This is useful if the device is in a network that configured to allow inbound connections only from specific ports.
  e.g.
	- `[0]`: bind to a random port;
	- `[9527]`: bind to port 9527;
	- `[0,9527]`: bind to a random port and port 9527.
- `upnp` (default: `false`): Request UPnP port mapping from supported router.
  This may not work for random port.

Make sure that all devices share the same config for entries `server` and `ipv6`.


## Host the rendezvous service yourself

### On Deno Deploy

Since the service is simply one TypeScript file, the easiest way to deploy is to use Deno Deploy playground.

1. [Create a new playground project](https://dash.deno.com/new) (you need a Deno Deploy account; the free plan is more than enough),
2. Copy-paste [this file](../edge/index.ts).
3. Pick a subdomain name or bind it to your own domain.
4. Set `server` field of the acp config files to the domain. (See the Advanced options section above)

### On any server

If you want to avoid cloud vendor lock-in, you can also run the service directly.
By doing so, the only difference is that the service is running on a single endpoint.

1. [Install Deno](https://deno.land/manual/getting_started/installation)
2. Clone the repo and run `deno run --allow-net=:8000 edge/index.ts`
3. (Recommended) Set up an HTTPS reverse proxy
4. Set `server` field of the acp config files to your domain. (See the Advanced options section above)
