package optimised

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

type RuleInfo struct {
	popID uint16
	scope int
}
type TrieNode struct {
	children [2]*TrieNode
	ruleInfo *RuleInfo
}
type Data struct {
	root *TrieNode
}

func NewData() *Data {
	return &Data{root: &TrieNode{}}
}

// extract a specific bit from a byte
func getBit(ip net.IP, n uint8) (uint8, error) {
	const lsbMask uint8 = 1
	if n >= 128 {
		return 0, fmt.Errorf("n must be less than 128")
	}
	// select the byte
	byteIndex := n / 8
	// calculate how many positions to shift the bits in our byte to the right so that the nth bit we are looking for becomes LSB
	bitIndexInByte := 7 - (n % 8)
	// shift the bits, determine whether our bit is 1 or 0
	if (ip[byteIndex]>>bitIndexInByte)&lsbMask == 1 {
		return 1, nil
	}
	return 0, nil
}

// makes sure that the subnet's mask len is IPv6 and that the address can be written as IPv6
func validateSubnet(subnet *net.IPNet) error {
	if subnet == nil {
		return fmt.Errorf("cannot insert nil subnet")
	}
	// make sure the mask is IPv6 (rule out IPv4 mask sizes)
	prefixLen, maskMaxBits := subnet.Mask.Size()
	if maskMaxBits != 128 {
		return fmt.Errorf("expected IPv6 subnet mask, got /%d with %d bits", prefixLen, maskMaxBits)
	}

	// convert IP to 16 byte form if needed (this can convert IPv4 into IPv6 if mask size is valid for IPv6)
	ip := subnet.IP.To16()
	if ip == nil {
		return fmt.Errorf("invalid IP address in subnet: %v", subnet.IP)
	}
	return nil
}

// helper for insert method to detect overlaps (check nodes below startNode for conflicting PoP IDs)
func checkDescendantConflicts(startNode *TrieNode, expectedPopID uint16) error {
	if startNode == nil {
		return nil
	}

	for _, child := range startNode.children {
		if child != nil {
			// do not skip the children directly
			if child.ruleInfo != nil && child.ruleInfo.popID != expectedPopID {
				return fmt.Errorf("found conflicting narrower rule at scope /%d with PoP %d", child.ruleInfo.scope, child.ruleInfo.popID)
			}
			// recursively check the subtree
			if err := checkDescendantConflicts(child, expectedPopID); err != nil {
				return err // return found error
			}
		}
	}
	return nil // no conflicts yayyyy
}

// eg. to insert 192.168.0.0/8 ppid:8 VS 192.0.0.0/8 ppid:111 exists
func checkSameNodeConflict(node *TrieNode, prefixLen int, subnetIP net.IP, popID uint16) error {
	if node.ruleInfo != nil && node.ruleInfo.popID != popID {
		// conflict found -> rule for this prefix exists with a different PoP ID
		return fmt.Errorf("conflict: rule for exact prefix %s/%d exists with different PoP %d (new PoP %d)",
			subnetIP, prefixLen, // Pass IP/len for better error message
			node.ruleInfo.popID, popID)
	}
	return nil // No conflict
}

// insert address into the trie in MSB order with prefix overlap checks
func (data *Data) insert(subnet *net.IPNet, popID uint16) error {

	if err := validateSubnet(subnet); err != nil {
		return err
	}

	prefixLen, _ := subnet.Mask.Size()
	ip := subnet.IP.To16()

	// traverse the path, crete nodes if needed
	currentNode := data.root
	for i := 0; i < prefixLen; i++ {
		// ancestor conflicts check
		if currentNode.ruleInfo != nil && currentNode.ruleInfo.popID != popID {
			// conflict found -> broader rule with different PoP ID exists
			return fmt.Errorf("conflict: new rule %s/%d (PoP %d) conflicts with broader rule at scope /%d (PoP %d)",
				subnet.IP, prefixLen, popID,
				currentNode.ruleInfo.scope, currentNode.ruleInfo.popID)
		}

		bit, err := getBit(ip, uint8(i))
		if err != nil {
			return fmt.Errorf("error getting bit %d for ip %v: %w", i, ip, err)
		}

		// a common path does not exist yet, create node
		if currentNode.children[bit] == nil {
			currentNode.children[bit] = &TrieNode{}
		}
		currentNode = currentNode.children[bit]
	}

	if err := checkSameNodeConflict(currentNode, prefixLen, subnet.IP, popID); err != nil {
		// conflict found -> rule with this exact prefix exists with a different PoP ID
		return err
	}

	if err := checkDescendantConflicts(currentNode, popID); err != nil {
		// conflict found -> narrower rule with different PoP ID exists
		return fmt.Errorf("conflict: new rule %s/%d (PoP %d) conflicts with existing narrower rule: %w",
			subnet.IP, prefixLen, popID,
			err)
	}

	// no conflicts
	currentNode.ruleInfo = &RuleInfo{
		popID: popID,
		scope: prefixLen,
	}
	return nil
}

func (data *Data) Route(ecs *net.IPNet) (pop uint16, scope int) {
	var bestPop uint16 = 0
	var bestScope int = -1

	if data == nil || data.root == nil || ecs == nil {
		return bestPop, bestScope
	}

	searchIP := ecs.IP.To16()
	if searchIP == nil {
		return bestPop, bestScope
	}

	currentNode := data.root
	// check root node
	if currentNode.ruleInfo != nil {
		bestPop = currentNode.ruleInfo.popID
		bestScope = currentNode.ruleInfo.scope
	}

	for i := 0; i < 128; i++ {
		bit, err := getBit(searchIP, uint8(i))
		if err != nil {
			fmt.Println(err)
			return bestPop, bestScope
		}

		if currentNode.children[bit] == nil {
			return bestPop, bestScope
		}
		currentNode = currentNode.children[bit]

		if currentNode.ruleInfo != nil {
			bestPop = currentNode.ruleInfo.popID
			bestScope = currentNode.ruleInfo.scope
		}
	}
	return bestPop, bestScope
}

func (data *Data) LoadRoutingData(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open routing ruleInfo file '%s': %w", filename, err)
	}
	defer file.Close()

	if data.root == nil {
		data.root = &TrieNode{}
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) != 2 {
			return fmt.Errorf("expected 2 fields (ECS IP, PopID), got %d in '%s'", len(parts), line)
		}

		cidrStr := parts[0]
		popIDStr := parts[1]

		_, ipNet, err := net.ParseCIDR(cidrStr)
		if err != nil {
			return fmt.Errorf("failed to parse CIDR '%s': %w", cidrStr, err)
		}

		popIDUint64, err := strconv.ParseUint(popIDStr, 10, 16)
		if err != nil {
			return fmt.Errorf("failed to parse PoP ID '%s': %w", popIDStr, err)
		}
		popID := uint16(popIDUint64)

		if err := data.insert(ipNet, popID); err != nil {
			return fmt.Errorf("error inserting rule (%s %d): %w", cidrStr, popID, err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading routing ruleInfo file '%s': %w", filename, err)
	}

	return nil
}
