package ui

import (
	"fmt"
	"strings"
	"time"
)

func Heading(title string) string {
	return title + "\n" + strings.Repeat("-", len(title))
}

func KV(key string, value any) string {
	return fmt.Sprintf("%-14s %v", key+":", value)
}

func Time(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format(time.RFC3339)
}
