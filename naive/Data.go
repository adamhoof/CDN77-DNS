package naive

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

type Data struct {
	Entries []RoutingEntry
}

func (d *Data) Route(ecs *net.IPNet) (pop uint16, scope int) {
	bestMatchPopID := uint16(0)
	longestMatchPrefixLen := -1

	// iterate through all entries
	for _, entry := range d.Entries {
		// check if the ECS IP is in the currently checked entry's subnet (mask & ecsIP, compare bytes of the net portion)
		if entry.Subnet.Contains(ecs.IP) {
			// get the source prefix length of the currently examined entry
			currentMatchPrefixLen, _ := entry.Subnet.Mask.Size()

			// check if the match is more specific than the previously found one
			if currentMatchPrefixLen > longestMatchPrefixLen {
				// update the best match
				longestMatchPrefixLen = currentMatchPrefixLen
				bestMatchPopID = entry.PopID
			}
		}
	}
	if longestMatchPrefixLen == -1 {
		return 0, 0
	}
	return bestMatchPopID, longestMatchPrefixLen
}

func (d *Data) LoadRoutingData(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open routing data file '%s': %w", filename, err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
		}
	}(file)

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

		d.Entries = append(d.Entries, RoutingEntry{ipNet, popID})
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading routing data file '%s': %w", filename, err)
	}

	return nil
}
