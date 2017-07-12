# peerpair
Application level network connectivity (and latency) alarming, using
UDP for point to point testing, and etcd as a central kv store for the
tests. Sends SNMP traps.

## Principles
* One-way testing, everyone should be ntp-synced.

One way testing is better as many of the configurations we're testing
are via LB/failover type IP connections with one client being routed
to one of many servers. With one way testing we can make the client(s)
send many packets and the each server only expect one.

* Latency testing

Recent hard-won experience says that even within the same DC there may
be transparent elements that are adding latency. That latency can make
otherwise healthy apps fail (1s might not seem a lot, but it was to
the app<->db combo that expected sub-1ms)

* Central config

One set of tests that everyone reads from. Global config, per-server
config and tests.

* UDP

We're not testing TCP 3-way handshakes. It could be added but nearly
all the failure scenarios we can expect don't care which IP protocol
is being used. UDP is simple and perfect for the one-way test model.

* SNMP

It's old, it's a bit awful, but it's widely supported and commonly
found. The code can be extended to other alarming types. SNMPv2c only
for now, the security/DDoS risk will need to be managed in your environment.

## Requirements

* An etcd cluster, with a v2 interface (http/JSON) enabled.
* The ability to run the peerpair client on each machine, and have the ports
opened towards each peer, and also access to the etcd cluster
* An SNMP Trap agent, something like net-snmp's snmptrapd will work
will if you don't already have HPOV or something of that ilk.

## Todo (in somewhat priority order)
* configurator program/web interface for tests
* https towards etcd
* SNMPv3
* etcd v3 interface
* non-SNMP alarming
* non-UDP connectivity testing

## Detailed Instructions
(to follow)
