
# TCP hole-punching and Edge function: how does all these work?

*This article assumes basic understanding of NAT (Network Address Translation)*


## Hole-punching

In order for two devices behind NATs to establish a direct connection, each needs to obtain a pair of address mapping from its NAT.
The mapping allows the device to "rent" a temporary public address from the NAT to associate with a device local address for a connection.
The following steps take place to create such mappings and use them for P2P connection:

1. Outbound connection to a public address, i.e. the rendezvous service, creates a pair of address mapping
2. The rendezvous service is able to see the temporary public address of this connection.
3. The rendezvous service informs each device of the temporary public address of the other.
4. Each device shut down the connection to the rendezvous service,
   and immediately *reuse* the connection's local address to open a connection to the other's temporary public address.
5. The new connections are able to reuse the mapping, and one of them will succeed.

<p align="center">
  <img src="../media/hole-punching.png" alt="hole-punching" />
</p>

Once the P2P connection is established, we obtain a direct tunnel where we can transfer files without a relay server.


## Hole-punching, but rendezvous at home

In a traditional approach, a rendezvous service runs as a single process,
because it needs to make connections with both side and inspect the connections to obtain both temporary public addresses.
Edge function changes this situation by providing computing endpoints distributed across the globe and optimized intra-connections among them.
Plus, all of these infrastrucures are available simply behind a simple message-passing interface.

<p align="center">
  <img src="../media/edge-network.png" alt="edge-network" />
</p>

Take a look at how an edge network handles the two scenarios below:

1. A and B are geographically close, so they are able to find a common cloest endpoint for rendezvous.
2. C and D are far from each other, but they can each find a cloest endpoint,
   and those two endpoints are able to talk to each other efficiently.
   This is like walking towards your laptop and starting a video conference meeting with your friend who lives at the other end of the globe.
