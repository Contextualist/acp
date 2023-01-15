
## Advanced options

`acp` stores config at `$HOME/.config/acp/config.json` (`%APPDATA%\acp\config.json` for Windows).
In general, you need to make sure that all devices use the same config.
After changing config on one device, run `acp --setup` to get the command for updating configs on other devices.

List of configurable options:

- `server` (default: `"https://acp.deno.dev"`): Endpoint for coordinating rendezvous
- `ipv6` (default: `false`): Establish P2P connection using IPv6 instead of IPv4.
  Note that both ends of a connection need to use the same IP protocol.

## Host the rendezvous service yourself

Since the service is simply one TypeScript file, the easiest way to deploy is to use Deno Deploy playground.

1. [Create a new playground project](https://dash.deno.com/new) (you need a Deno Deploy account; the free plan is more than enough),
2. Copy-paste [this file](../edge/index.ts).
3. Pick a subdomain name or bind it to your own domain.
4. Set `server` field of the acp config files to the domain. (See the section above)
