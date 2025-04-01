package main

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

func validateSubnet(subnet *net.IPNet) error {
	if subnet == nil {
		return fmt.Errorf("cannot insert nil subnet")
	}
	// make sure the mask is IPv6 (rule out IPv4 mask sizes)
	prefixLen, maskMaxBits := subnet.Mask.Size()
	if maskMaxBits != 128 {
		return fmt.Errorf("expected IPv6 subnet, got /%d with %d bits", prefixLen, maskMaxBits)
	}

	// convert IP to 16 byte form if needed (this can convert IPv4 into IPv6 if mask size is valid for IPv6)
	ip := subnet.IP.To16()
	if ip == nil {
		return fmt.Errorf("invalid IP address in subnet: %v", subnet.IP)
	}
	return nil
}

// Insert address into our trie, *MSB* order
func (data *Data) Insert(subnet *net.IPNet, popID uint16) error {

	if err := validateSubnet(subnet); err != nil {
		return err
	}

	prefixLen, _ := subnet.Mask.Size()
	ip := subnet.IP.To16()

	// traverse the path (prefix length nodes), crete nodes if needed
	node := data.root
	for i := 0; i < prefixLen; i++ {
		bit, err := getBit(ip, uint8(i))
		if err != nil {
			return err
		}
		if node.children[bit] == nil {
			node.children[bit] = &TrieNode{}
		}
		node = node.children[bit]
	}

	// store the rule ruleInfo at the node corresponding to the prefix length
	node.ruleInfo = &RuleInfo{
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
	if currentNode.ruleInfo != nil {
		bestPop = currentNode.ruleInfo.popID
		bestScope = currentNode.ruleInfo.scope
	}

	for i := 0; i < 128; i++ {
		if currentNode.ruleInfo != nil {
			bestPop = currentNode.ruleInfo.popID
			bestScope = currentNode.ruleInfo.scope
		}

		bit, err := getBit(searchIP, uint8(i))
		if err != nil {
			fmt.Println(err)
			return bestPop, bestScope
		}

		// avoid pointing to nil if we hit the end
		if currentNode.children[bit] == nil {
			return bestPop, bestScope
		}
		currentNode = currentNode.children[bit]
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
			return fmt.Errorf("failed to parse ECS IP '%s': %w", cidrStr, err)
		}

		popIDUint64, err := strconv.ParseUint(popIDStr, 10, 16)
		if err != nil {
			return fmt.Errorf("failed to parse PoP ID '%s': %w", popIDStr, err)
		}
		popID := uint16(popIDUint64)

		if err := data.Insert(ipNet, popID); err != nil {
			return fmt.Errorf("error inserting rule (%s %d): %w", cidrStr, popID, err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading routing ruleInfo file '%s': %w", filename, err)
	}

	return nil
}
