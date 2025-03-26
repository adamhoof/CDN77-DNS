## Needed to research for the DNS part, TLDR:

Authoritative DNS server -> the final DNS server that is able to return the right answer unlike recursive DNS resolver, which might not have it cached and ask other recursive <br> 
ECS -> Extension Mechanisms for DNS Client Subnet -> extension that contains a part of the client's IP as a part of DNS query <br>
RFC 7871, Section 7.2.1 -> how ECS requests are processed ((not) implemented functionality) and privacy concerns (how much of the subnet is sent?) <br>
DNS query types -> (type A means IPv4 address is returned + IPv6 ECS -> request contains client's IPv6 subnet) => query typu A s IPv6 ECS means that the request contains client's IPv6 subnet and the client is requesting IPv4 address <br>
PoP -> point of presence, CDN server which is picked by DNS server using geolocation data like ECS to identify the right one <br>
Source prefix length -> how many bits of the original client address did the RR supporting ECS decide to include in the ECS option as it is passed onto the Authoritative DNS (0xmasked IP) <br>
Scope prefix length -> returned by the Authoritative DNS to the RR to indicate how broadly can the answer be applied -> RR then knows that this answer is appliacble to any client within the scope given by the ScPL <br>
DNS resolver vs DNS authoritative server -> caches and relays answers vs source of truth, owns DNS records <br>

## Needed to research for the nginx part, TLDR:

Nginx -> high performance software efficient under heavy load, event driven arch, can function as a web server, reverse proxy with load balancer, cache etc. -> those are configurable<br>
Forward vs Reverse proxy -> acts on behalf of the client, eg. VPN that hides client IP vs acts on behalf of the server, eg. reverse proxy with load balancing, caching etc.<br>

## High level questions:

Why did we choose to solve it this way?<br>
What did we get stuck at, how did we overcome it? How could it be solved differently?<br>
How would the solution scale?<br>
Performance, code maintainability, security...<br>
What parts of the solution are optimal, which are not?<br>
What could be imporoved and why not improve it straight up?<br>
How long did the task take? (research, implementation, debug)<br>
How did we think about the task?<br>
What did we come up with and what did we threw away?<br>
What would we do if it went to production?<br>
