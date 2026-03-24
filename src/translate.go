package main

import (
	"bufio"
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	litematicaTitlePattern = regexp.MustCompile(`Material List for schematic '([^']+)'`)
	litematicaRowPattern   = regexp.MustCompile(`^\s*\|\s*(.+?)\s*\|\s*(\d+)\s*\|\s*(\d+)\s*\|\s*(\d+)\s*\|`)
)

func parseLitematicaList(data []byte, fallbackName string, honorAvailable bool) ([]item, string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	items := []item{}
	listName := strings.TrimSpace(strings.TrimSuffix(filepath.Base(fallbackName), filepath.Ext(fallbackName)))

	for scanner.Scan() {
		line := scanner.Text()

		if match := litematicaTitlePattern.FindStringSubmatch(line); len(match) == 2 && listName == "" {
			listName = strings.TrimSpace(match[1])
		}

		match := litematicaRowPattern.FindStringSubmatch(line)
		if len(match) != 5 {
			continue
		}

		itemName := strings.TrimSpace(match[1])
		if itemName == "" || strings.EqualFold(itemName, "Item") {
			continue
		}

		total, err := strconv.Atoi(match[2])
		if err != nil {
			return nil, "", fmt.Errorf("invalid total for %q", itemName)
		}
		available, err := strconv.Atoi(match[4])
		if err != nil {
			return nil, "", fmt.Errorf("invalid available value for %q", itemName)
		}

		gathered := 0
		contributions := []contribution{}
		if honorAvailable {
			gathered = available
			if gathered > total {
				gathered = total
			}
			if gathered > 0 {
				contributions = []contribution{{
					Username: legacyContributionUser,
					Amount:   gathered,
				}}
			}
		}

		items = append(items, item{
			Name:          itemName,
			Target:        total,
			Gathered:      gathered,
			Claims:        []claim{},
			Contributions: contributions,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, "", err
	}
	if len(items) == 0 {
		return nil, "", fmt.Errorf("no litematica rows found in uploaded file")
	}
	if listName == "" {
		listName = "Imported Litematica List"
	}

	return items, listName, nil
}
