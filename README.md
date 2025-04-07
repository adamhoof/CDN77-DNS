# DNS task

### TLDR research aka what I needed to even start

Authoritative DNS server -> the final DNS server that is able to return the right answer unlike recursive DNS resolver, which might not have it cached and ask other recursive <br>
ECS -> Extension Mechanisms for DNS Client Subnet -> extension that contains a part of the client's IP as a part of DNS query <br>
RFC 7871, Section 7.2.1 -> how ECS requests are processed ((not) implemented functionality) and privacy concerns (how much of the subnet is sent?) <br>
"Query typu A s IPv6 ECS" -> DNS type A query means IPv4 address is returned; IPv6 ECS means request contains client's IPv6 subnet) => request contains a portion of a client's IPv6 subnet and the client is requesting IPv4 address <br>
PoP -> Point of Presence, CDN server which is picked by DNS server using geolocation data like ECS to identify the (typically) closest one <br>
Source prefix length -> how many bits of the original client address did the RR supporting ECS decide to include in the ECS option as it is passed onto the Authoritative DNS (0xmasked IP) <br>
Scope prefix length -> returned by the Authoritative DNS to the RR to indicate how broadly can the answer be applied -> RR then knows that this answer is applicable to any client within the scope given by the ScPL <br>
DNS resolver VS DNS authoritative server -> caches and relays answers VS source of truth, owns DNS records <br>
Cache poisoning -> In RR context for example, it's cache becomes poisoned when the RR thinks that ie. PoP ID 10 is correct for the entire /8 prefix, meaning anyone with more specific prefix will receive PoP ID 10 as a result as well. <br>
RFC conflicts resolution method using "Detect and Error" -> prevents cache poisoning by rejecting conflicting overlapping rules that create ambiguous server responses – like simultaneously having /8 -> PoP A and /16 -> PoP B. This is suboptimal but correct, the broad answers are caused simply by a lack of granular data.

---

### Naive solution: unsatisfactory time complexity O(n), space complexity O(n), where n is number of routing data entries
- iterate through all the routing rules
- find one that matches the incoming ECS IP AND has the longest prefix length

---

### Optimised solution: satisfactory time complexity O(ipv6l) = O(1) < O(n), space complexity O(ipv6l * n), where ipv6l is the length of IPv6 address = 128, n is the number of routing data entries
- build a binary trie structure, where each level represents a specific bit position of an address in binary form and each node at that level represents either 1 or 0 of an address
- check for overlapping rules according to RFC by throwing an error for DNS server admin to check
- implement search by traversing the trie down, just following the existing path in the trie

#### Optimised solution thought processes:
Core idea -> addresses repeat a lot and storing each will create a lot of redundancy, looks like a tree of some sort (this is definitely more space efficient, but let's see time complexity)

1. How to split the address into nodes? -> store numbers in each node to represent 4 bits of address
   - Issue: This allowed shallow Trie (128 / 4 = 32 levels only at most), 4 children per node BUT did not align with prefix lengths, we would have hard time storing and representing prefixes that are not multiples of 4, like 31
   - Solution: We can store just one bit per node instead of numbers (which makes it into a Binary Trie), aligning with prefix exactly: prefix 31 = trie level 31, much more granular!
2. How to store the PoP ID? -> specific nodes will each either have or not have PoP ID assigned to them as per the routing data example
3. Space complexity looks good, what about time complexity? -> It will be dependent on the number of levels (prefix length) because of the trie traversal and nothing else, that should be way better than O(n), where n is number of records, we are not even dependent on records number at all!
4. How will the algorithm work then? -> Hmm, so there is the initial trie build (based on the given routing data) and then the search
   - **Trie build**: We traverse the trie down and build nodes that do not yet exist. If a duplicate path is found, it might be rewritten to simulate updated record? But that is not that important.
     - Hmm, speaking of duplicates, what if we find more specific path (greater prefix) with the same PoP ID, ie. 198? To optimise space, we will probably delete the less specific path? NOO, this would remove a valid path for requests with much less specific prefix, keep it.
   - **Trie search**: We traverse the trie down and along the path until we hit ECS mask, keep track of a node that has PoP ID AND is the most specific == further down the trie == larger prefix. When we hit the last possible one (depth equal to the ECS IP length) we will safely tell the most concrete prefix and return its scope prefix length and PoP ID.
     - Invalid Issue: Wait but if we only traverse until ECS IP mask, we might miss a path that contains this ECS address AND is more specific
     - Invalid Solution: --FROM FUTURE WRONG MARK--> ECS IP = 4bits:4bits.../50, but path = 4bits:4bits.../90 contains this ECS IP AND is more specific <--FROM FUTURE WRONG MARK-- So we go until the trie offers a path down.
       - Issue: Wait but this means that when the ECS IP mask ends (it is like a key that guides the path), we have multiple paths to choose from
       - Solution1: We will need to split at that point, find all, sort, find the most specific one? This means performance hit, but it might be necessary, because if we search just the first path available (using DFS ie) and not all of them, we might choose a prefix that is very broad in the end => not good for the customer!
       - Solution2: Maybe choose a middle ground solution that tells us "this is specific enough prefix" that will act as a block, where the average case will be searching 1/2 of the paths instead of all of them?
     - Actual Solution: Hold on, the search might actually work differently and easier ---FIXED WRONG--> /50 contains /90, not reverse, /50 IS BROADER than /90 <---FIXED WRONG--. When the Authoritative DNS performs the lookup, it takes this ECS IP as a whole key and just follows it down the path, so the 0's will either lead us to nil or to the max, 128 length. 
       - Hmm and this actually gives us the time complexity, O(ipv6l), where ipv6l is the length of IPv6 address, that is 128 => O(1), nice
     - Space complexity looks like O(N*ipv6l), when we consider the worst case, where essentially each rule has its own path of ipv6l = 128 nodes. The 128 is a big factor, it could get better probably?
       - Issue: Hmmm so if we have 2 paths for rules /40 and /60, there will be 20 redundant nodes not having any PoP ID, serving no purpose. We could just connect /40 to /60, but this would lose accuracy?
       - Solution: We could store the remaining bits of the address somewhere, perhaps in the end /40 node that connects to the /60, this allows merging the prefix and not losing accuracy -> This is pretty hard to pull of, let's leave it for now so that it does not take ages, but we know this is a valid thing to solve in real world scenario.
     - Overlaps as per RFC need to be also solved. Good thing is that if one prefix contains the other and both have same PoP ID (like /20 and /40, PoP 198) our trie automatically finds the best possible PoP ID with prefix because it follows the ECS IP down and notes only the most optimal PoP ID with corresponding scope prefix.
       - Issue: Overlaps that become problematic are, when one contains the other, but have different PoP IDs -> 2001:2000::/40 229 VS 2001:2000::/20 19.
       - Solution fork: When inserting the rules, this should trigger an error (or split broad prefix), according to the RFC.
         - Not selected solution: Prefix deaggregation would violate the requirement for memory efficiency, since a lot of rules would be added as a result of this deaggregation. It is also very performance hungry. 
         - Selected solution: Detect and Error is much more suitable for this task, since it does not use more memory and during the insertion only goes through the trie to check if conflicting rules exist. 

---

### Even more optimised solution - what/why did I not implement, what would it improve
- What did I not implement?
  - Path reduction using Radix Trie
    - What would it improve? Close to O(n) space complexity would be achieved, because we would reduce the need for redundant nodes that do not represent any {PoP ID;scope prefix length} pair
    - Why did I not implement it? It would be pretty hard, wanted to show I know it could be done to optimise further and how to approach it
---

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