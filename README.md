# DNS task

[![CI Pipeline Status](https://github.com/adamhoof/CDN77-DNS/actions/workflows/CI_pipeline.yaml/badge.svg)](https://github.com/adamhoof/CDN77-DNS/actions/workflows/CI_pipeline.yaml)


**Contents** <br>
[TLDR Research](#tldr-research) <br>
[Naive solution](#naive-solution) <br>
[Optimised solution](#optimised-solution) <br>
[Even more optimised solution](#even-more-optimised-solution-not-implemented) <br>
[Approximate time requirements](#approximate-time-requirements) <br>
[CI pipeline](#ci-pipeline)

## TLDR research

Authoritative DNS server vs recursive resolver -> Authoritative DNS server is the last in the lookup chain that is the owner of DNS records (definitive answers to queries), unlike recursive resolver, which caches these answers and queries other servers. 
ECS, Extension Mechanisms for DNS Client Subnet -> extension which allows containing a part of the client's IP as a part of DNS query <br>
RFC 7871, Section 7.2.1 -> details how incoming ECS queries are processed, how to format ECS responses, how to prevent conflicting overlapping rules.
"Query typu A s IPv6 ECS" -> DNS type A query means IPv4 address is returned; IPv6 ECS means request contains client's IPv6 subnet) => request contains a portion of a client's IPv6 subnet and the client is requesting IPv4 address <br>
PoP -> Point of Presence, CDN server which is picked by DNS server using geolocation data like ECS to identify the closest one (typically determined by lowest latency) <br>
Source prefix length -> the number of bits of the client subnet provided by the RR included in ESC option sent to the Authoritative DNS.
Scope prefix length -> returned by the Authoritative DNS to the RR to indicate how broadly can the answer be applied -> RR then knows that this answer is applicable to any client within the scope given by the scope prefix length <br>
CIDR -> Classless Inter-Domain Routing is a way to represent IP addresses and their prefix in modern networking. More flexible than classful system (A, B, C). Formatted as IP/prefixlen. 
Cache poisoning -> In ECS context for example, RR's cache becomes poisoned when the RR thinks that ie. PoP ID 10 is correct for the entire /8 prefix, meaning anyone with more specific prefix will receive PoP ID 10 as a result as well. <br>
RFC conflicts resolution method: Detect and Error -> reject conflicting overlapping rules that create ambiguous server responses, like simultaneously having /8 -> PoP 5 and /16 -> PoP 10. The order of requests unfortunately determines, whether the more specific or broader rule becomes cached. This is suboptimal but correct (Authoritative DNS server is consistent), these issues are caused simply by the lack of granular data. <br>
RFC conflicts resolution method: Deaggregation -> replaces conflicting broad rule (eg. /8, PoP 5) with multiple smaller non-overlapping prefixes that cover the broad rule range except for the area of the conflicting narrower rule (eg. /16, PoP 10).

---

## Naive solution
### Asymptotic complexities (where n is the number of routing data entries)
**Time complexity**: O(n), unsatisfactory <br>
**Space complexity**: O(n) <br><br>

### Main ideas
- iterate through all the routing data
- find one that matches the incoming ECS IP AND is the most specific

---

## Optimised solution
### Asymptotic complexities (where n is the number of routing data entries, ipv6l is the bit length of IPv6 = 128)
**Time complexity**: O(ipv6l) = O(1) < O(n), satisfactory <br>
**Space complexity**: O(ipv6l * n), ok, but got worse. ipv6l is just a multiplicative constant, but it is not small, improve? <br><br>

### Main ideas
- Core -> addresses repeat a lot and storing each will create a lot of redundancy in searching and space, looks like a tree of some sort (the lookup in trees is definitely more efficient if designed carefully).
  - build a binary trie structure, where each level represents a specific bit position of an address in binary form and each node at that level represents either 1 or 0 of an address
  - check for overlapping rules according to RFC by throwing an error for DNS server admin to check
  - implement search by traversing the trie down, just following the existing path in the trie

**Note: checkout TestInsertConflicts, TestInsertAndRoute in Optimised_test.go**<br><br>

### Thought processes (literary):
1. **How to split the address into nodes?** -> store numbers in each node to represent 4 bits of address
   - Issue: This allowed shallow Trie (128 / 4 = 32 levels only at most), 4 children per node BUT did not align with prefix lengths, we would have hard time storing and representing prefixes that are not multiples of 4, like 31
   - Solution: We can store just one bit per node instead of numbers (which makes it into a binary trie), aligning with prefix exactly: prefix 31 = trie level 31, much more granular!
2. **How to store the PoP ID?** -> specific nodes will each either have or not have PoP ID assigned to them as per the routing data example
3. **Space complexity looks good, what about time complexity?** -> It will be dependent on the number of levels (prefix length) because of the trie traversal and nothing else, that should be way better than O(n), where n is number of records, we are not even dependent on records number at all!
4. **How will it work then?** -> Hmm, so there is the initial trie build (based on the given routing data) and then the search (route)
   - **Trie build**: We traverse the trie down and build nodes that do not yet exist, connecting to the existing ones (pointer). Each node will point to max 2 child nodes (binary trie), representing either 1 or 0 of the address.
     - Speaking of duplicates, what if we find more specific path (greater prefix) with the same PoP ID, ie. 198? To optimise space, we will probably delete the less specific path? NOO, this would remove a valid path for requests with less specific prefix, keep it.
     - Space complexity looks like O(n*ipv6l), when we consider the worst case, where essentially each rule has its own path of ipv6l = 128 nodes. The 128 is a big factor, it could get better probably? For details, jump to [even more optimised solution](#even-more-optimised-solution-not-implemented).
       - Issue: Hmmm so if we have 2 paths for rules /40 and /60, there will be 20 redundant nodes not having any PoP ID, serving no purpose. We could just connect /40 to /60, but this would lose accuracy?
       - Solution: We could store the remaining bits of the address somewhere, perhaps in the child node that connects to the parent (/40 -> /60, store skipped bits in /60), this allows merging the prefix and not losing accuracy. 
     - Overlaps as per RFC need to be also solved. Good thing is that if one prefix contains the other and both have same PoP ID (like /20 contains /40, both PoP 198) our trie automatically finds the best possible PoP ID with prefix because it follows the ECS IP down and updates the most optimal PoP ID with corresponding scope prefix.
         - Issue: Prefix overlaps become problematic when one contains the other, but have different PoP IDs -> /20 19 contains /40 229, but PoPs are different.
         - Solution fork: When inserting the rules, this should trigger an error (or split broad prefix), according to the RFC.
             - Fork1 (not selected solution): Prefix deaggregation would violate the requirement for memory efficiency, since a lot of rules would be added as a result of prefix deaggregation. It is also very performance hungry to detect and correct these conflicts!
             - Fork2 (selected solution): Detect and Error is much more suitable for this task, since it does not use more memory and during the insertion only goes through the trie to check if conflicting rules exists (either broader, exact or narrower rule exists relative to the inserted one, that would be conflicting).
   - **Trie search**: We traverse the trie down and along the path until we hit ECS mask, keep track of a node that has PoP ID AND is the most specific == further down the trie == larger prefix. When we hit the last possible one (depth equal to the ECS IP length) we will safely tell the most concrete prefix and return its scope prefix length and PoP ID.
     - Invalid Issue: Wait but if we only traverse until ECS IP mask, we might miss a path that contains this ECS address AND is more specific
     - Invalid Solution: --FROM FUTURE WRONG MARK--> ECS IP = 4bits:4bits.../50, but path = 4bits:4bits.../90 contains this ECS IP AND is more specific <--FROM FUTURE WRONG MARK-- So we go until the trie offers a path down.
       - Issue: Wait but this means that when the ECS IP mask ends (it is like a key that guides the path), we have multiple paths to choose from
       - Solution1: We will need to split at that point, find all, sort, find the most specific one? This means performance hit, but it might be necessary, because if we search just the first path available (using DFS ie) and not all of them, we might choose a prefix that is very broad in the end => not good for the customer!
       - Solution2: Maybe choose a middle ground solution that tells us "this is specific enough prefix" that will act as a block, where the average case will be searching 1/2 of the paths instead of all of them?
     - Actual Solution: Hold on, the search might actually work differently and easier ---FIXED WRONG--> /50 contains /90, not reverse, /50 IS BROADER than /90 <---FIXED WRONG--. When the Authoritative DNS performs the lookup, it takes this ECS IP as a whole key and just follows it down the path, so the 0's will either lead us to nil or to the max, 128 length. 
       - Hmm and this actually gives us the time complexity, O(ipv6l), where ipv6l is the length of IPv6 address, that is 128 => O(1), nice
---

## Even more optimised solution (not implemented)
### Asymptotic complexities (where n is the number of routing data entries, ipv6l is the bit length of IPv6 = 128)
**Time complexity**: O(ipv6l) = O(1) < O(n), satisfactory <br>
**Space complexity**: O(n), improved, probably can not be better <br><br>

### Main ideas
Core idea -> take the binary trie and transform it into binary radix trie - adds path compression. As addressed in the optimised solution, it will avoid creating redundant nodes not representing any PoP ID x prefix length rules, like the ones between /40 and /60 prefixes.<br><br>
### Thought process (literary):
1. **Rethink node/data storage**:
   - Issue: Path compression (connecting /40 to /60 directly) would lose bit info.
   - Solution: The compression nodes store info about the edge ->leading<- to them (the sequence of bits and its length), something like edge.bits []byte, edge.length uint8.
2. **Trie build scenarios**:
   - Path does not exist at all -> We insert at /40, then attempt /60. This works, we detect nil pointer at children.[next_bit] and just connect the /60 node directly, store the bits into edge.bits of this /60 child node.
   - Path exists via a child -> We attempt to insert new rule.
     - Issue: Now we do not have a chain of nodes and can't just follow the path and insert the rule at position (ie. wanna insert /50, but /40 -> /60 directly connected).
     - Solution fork: Compare the new prefix bits (from current depth) with the existing child edge bits. 
       - Fork1: Matches directly the whole prefix -> (/50 wants to be inserted, we have /40 -> /60, everything is ok until /50) -> New node will be created containing the rule. /40 parent will point to this node, this node will point to the /60 child. 
       - Fork2: Stops matching somewhere in the process -> (/50 wants to be inserted, we have /40 -> /60, but it breaks at /45 ie.) -> We need to split the edge, so /40 -> /45 -> /50 -> /60 will be the result.
       - Fork3: The new rule is longer (/70 wants to be inserted, but we have /40 -> /60), we will add it after the last node in the chain.
3. **Trie search**:
   - Now that we have a complete binary radix trie, the lookup should not be super hard. We only need to account for the fact that nodes now store skipped bits, against which we need to check instead of just following the path of single bit child pointers.
       
---

## CI pipeline

**Dir**: .github/workflows <br> 
**TLDR** description: Using simple GitHub Actions workflow to automatically check code formatting with gofmt, build the project and run all tests whenever code is pushed to the master branch or a pull request targeting master is updated.
---

## Approximate time requirements:
**Research** (topics, terms): 2h <br>
**Analysis**: 1h naive + 4h optimised: 5h <br>
**Documentation** (thought process capture, ideas, research): 4h <br>
**Implementation**: 1h naive + 7h optimised + 1h tests: 9h <br>

# High level questions to answer

Why did we choose to solve it this way?<br>
What did we get stuck at, how did we overcome it? How could it be solved differently?
How would the solution scale?<br>
Performance, code maintainability, security...<br>
What parts of the solution are optimal, which are not?<br>
What could be improved and why not improve it straight up?<br>
How long did the task take? (research, implementation, debug)<br>
How did we think about the task?<br>
What did we come up with and what did we threw away?<br>
What would we do if it went to production? -> Proper testing has already taken place on a pre-prod environemnt<br>