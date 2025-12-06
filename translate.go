//go:build translate

package translate

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type Item struct {
	Name     string  `json:"name"`
	Target   int     `json:"target"`
	Gathered int     `json:"gathered"`
	Claims   *string `json:"claims"`
}

func main() {
	// Read the input file
	data, err := os.ReadFile("material_list_2025-12-04_10.09.24.txt")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	lines := strings.Split(string(data), "\n")
	var items []Item

	// Regex to match data rows in the table
	// Format: | Item Name | Total | Missing | Available |
	rowRegex := regexp.MustCompile(`^\s*\|\s*(.+?)\s*\|\s*(\d+)\s*\|\s*(\d+)\s*\|\s*(\d+)\s*\|`)

	for _, line := range lines {
		matches := rowRegex.FindStringSubmatch(line)
		if matches != nil && len(matches) == 5 {
			itemName := strings.TrimSpace(matches[1])

			// Skip header rows
			if itemName == "Item" || itemName == "Material List for schematic 'Mountain - Fixed' (1 of 1 regions)" {
				continue
			}

			total, err1 := strconv.Atoi(matches[2])
			available := 0

			if err1 == nil && err2 == nil {
				item := Item{
					Name:     itemName,
					Target:   total,
					Gathered: available,
					Claims:   nil,
				}
				items = append(items, item)
			}
		}
	}

	// Marshal to JSON with indentation
	jsonData, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
		os.Exit(1)
	}

	// Write to items.json
	err = os.WriteFile("items.json", jsonData, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully converted %d items to items.json\n", len(items))
}
