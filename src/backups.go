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
	lists, err := readLists()
	if err != nil || len(lists) == 0 {
		return
	}

	if err := os.MkdirAll(backupsDir, 0755); err != nil {
		log.Printf("backup mkdir error: %v", err)
		return
	}

	timestamp := time.Now().UTC().Format("2006-01-02T15-04-05Z07-00")
	filename := fmt.Sprintf("lists-%s.json", timestamp)
	path := filepath.Join(backupsDir, filename)

	if err := writeJSONFile(path, lists); err != nil {
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
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "lists-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		timestampStr := strings.TrimPrefix(entry.Name(), "lists-")
		timestampStr = strings.TrimSuffix(timestampStr, ".json")
		timestampStr = strings.ReplaceAll(timestampStr, "-", ":")
		timestampStr = strings.Replace(timestampStr, ":", "-", 2)
		timestampStr = strings.Replace(timestampStr, ":", "-", 1)
		t, err := time.Parse("2006-01-02T15-04-05Z07:00", timestampStr)
		if err != nil {
			continue
		}
		backups = append(backups, backupFile{name: entry.Name(), time: t})
	}

	if len(backups) == 0 {
		return
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].time.Before(backups[j].time)
	})

	now := time.Now()
	toKeep := map[string]bool{backups[0].name: true}

	var (
		recent  = 2 * time.Hour
		hourly  = 24 * time.Hour
		daily   = 7 * 24 * time.Hour
		weekly  = 30 * 24 * time.Hour
		monthly = 365 * 24 * time.Hour
	)

	for _, backup := range backups {
		if now.Sub(backup.time) <= recent {
			toKeep[backup.name] = true
		}
	}

	hourlyBuckets := make(map[string]backupFile)
	for _, backup := range backups {
		age := now.Sub(backup.time)
		if age > recent && age <= hourly {
			bucket := backup.time.Truncate(time.Hour).Format(time.RFC3339)
			if existing, ok := hourlyBuckets[bucket]; !ok || backup.time.After(existing.time) {
				hourlyBuckets[bucket] = backup
			}
		}
	}
	for _, backup := range hourlyBuckets {
		toKeep[backup.name] = true
	}

	dailyBuckets := make(map[string]backupFile)
	for _, backup := range backups {
		age := now.Sub(backup.time)
		if age > hourly && age <= daily {
			bucket := backup.time.Truncate(24 * time.Hour).Format(time.RFC3339)
			if existing, ok := dailyBuckets[bucket]; !ok || backup.time.After(existing.time) {
				dailyBuckets[bucket] = backup
			}
		}
	}
	for _, backup := range dailyBuckets {
		toKeep[backup.name] = true
	}

	weeklyBuckets := make(map[string]backupFile)
	for _, backup := range backups {
		age := now.Sub(backup.time)
		if age > daily && age <= weekly {
			_, week := backup.time.ISOWeek()
			bucket := fmt.Sprintf("%d-W%d", backup.time.Year(), week)
			if existing, ok := weeklyBuckets[bucket]; !ok || backup.time.After(existing.time) {
				weeklyBuckets[bucket] = backup
			}
		}
	}
	for _, backup := range weeklyBuckets {
		toKeep[backup.name] = true
	}

	monthlyBuckets := make(map[string]backupFile)
	for _, backup := range backups {
		age := now.Sub(backup.time)
		if age > weekly && age <= monthly {
			bucket := backup.time.Format("2006-01")
			if existing, ok := monthlyBuckets[bucket]; !ok || backup.time.After(existing.time) {
				monthlyBuckets[bucket] = backup
			}
		}
	}
	for _, backup := range monthlyBuckets {
		toKeep[backup.name] = true
	}

	yearlyBuckets := make(map[string]backupFile)
	for _, backup := range backups {
		age := now.Sub(backup.time)
		if age > monthly {
			bucket := backup.time.Format("2006")
			if existing, ok := yearlyBuckets[bucket]; !ok || backup.time.After(existing.time) {
				yearlyBuckets[bucket] = backup
			}
		}
	}
	for _, backup := range yearlyBuckets {
		toKeep[backup.name] = true
	}

	for _, backup := range backups {
		if !toKeep[backup.name] {
			_ = os.Remove(filepath.Join(backupsDir, backup.name))
		}
	}
}
