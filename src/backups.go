package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func scheduleBackups() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		performBackup()
		<-ticker.C
	}
}

func performBackup() {
	items, err := readItems()
	if err != nil || len(items) == 0 {
		return
	}

	if err := os.MkdirAll(backupsDir, 0755); err != nil {
		log.Printf("backup mkdir error: %v", err)
		return
	}

	timestamp := time.Now().UTC().Format("2006-01-02T15-04-05Z07-00")
	filename := fmt.Sprintf("items-%s.json", timestamp)
	path := filepath.Join(backupsDir, filename)

	if err := writeJSONFile(path, items); err != nil {
		log.Printf("backup write error: %v", err)
		return
	}

	cleanupBackups()
}

func cleanupBackups() {
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		return
	}

	type backupFile struct {
		name string
		time time.Time
	}

	var backups []backupFile
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "items-") && strings.HasSuffix(e.Name(), ".json") {
			timestampStr := strings.TrimPrefix(e.Name(), "items-")
			timestampStr = strings.TrimSuffix(timestampStr, ".json")
			timestampStr = strings.ReplaceAll(timestampStr, "-", ":")
			timestampStr = strings.Replace(timestampStr, ":", "-", 2)
			timestampStr = strings.Replace(timestampStr, ":", "-", 1)
			t, err := time.Parse("2006-01-02T15-04-05Z07:00", timestampStr)
			if err != nil {
				continue
			}
			backups = append(backups, backupFile{name: e.Name(), time: t})
		}
	}

	if len(backups) == 0 {
		return
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].time.Before(backups[j].time)
	})

	now := time.Now()
	toKeep := make(map[string]bool)
	toKeep[backups[0].name] = true

	var (
		recent  = 2 * time.Hour
		hourly  = 24 * time.Hour
		daily   = 7 * 24 * time.Hour
		weekly  = 30 * 24 * time.Hour
		monthly = 365 * 24 * time.Hour
	)

	for _, b := range backups {
		if now.Sub(b.time) <= recent {
			toKeep[b.name] = true
		}
	}

	hourlyBuckets := make(map[string]backupFile)
	for _, b := range backups {
		age := now.Sub(b.time)
		if age > recent && age <= hourly {
			bucket := b.time.Truncate(time.Hour).Format(time.RFC3339)
			if existing, exists := hourlyBuckets[bucket]; !exists || b.time.After(existing.time) {
				hourlyBuckets[bucket] = b
			}
		}
	}
	for _, b := range hourlyBuckets {
		toKeep[b.name] = true
	}

	dailyBuckets := make(map[string]backupFile)
	for _, b := range backups {
		age := now.Sub(b.time)
		if age > hourly && age <= daily {
			bucket := b.time.Truncate(24 * time.Hour).Format(time.RFC3339)
			if existing, exists := dailyBuckets[bucket]; !exists || b.time.After(existing.time) {
				dailyBuckets[bucket] = b
			}
		}
	}
	for _, b := range dailyBuckets {
		toKeep[b.name] = true
	}

	weeklyBuckets := make(map[string]backupFile)
	for _, b := range backups {
		age := now.Sub(b.time)
		if age > daily && age <= weekly {
			_, week := b.time.ISOWeek()
			bucket := fmt.Sprintf("%d-W%d", b.time.Year(), week)
			if existing, exists := weeklyBuckets[bucket]; !exists || b.time.After(existing.time) {
				weeklyBuckets[bucket] = b
			}
		}
	}
	for _, b := range weeklyBuckets {
		toKeep[b.name] = true
	}

	monthlyBuckets := make(map[string]backupFile)
	for _, b := range backups {
		age := now.Sub(b.time)
		if age > weekly && age <= monthly {
			bucket := b.time.Format("2006-01")
			if existing, exists := monthlyBuckets[bucket]; !exists || b.time.After(existing.time) {
				monthlyBuckets[bucket] = b
			}
		}
	}
	for _, b := range monthlyBuckets {
		toKeep[b.name] = true
	}

	yearlyBuckets := make(map[string]backupFile)
	for _, b := range backups {
		age := now.Sub(b.time)
		if age > monthly {
			bucket := b.time.Format("2006")
			if existing, exists := yearlyBuckets[bucket]; !exists || b.time.After(existing.time) {
				yearlyBuckets[bucket] = b
			}
		}
	}
	for _, b := range yearlyBuckets {
		toKeep[b.name] = true
	}

	for _, b := range backups {
		if !toKeep[b.name] {
			_ = os.Remove(filepath.Join(backupsDir, b.name))
		}
	}
}
