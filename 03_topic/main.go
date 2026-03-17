package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"

	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"
	yc "github.com/ydb-platform/ydb-go-yc"
)

func main() {
	usersFlag := flag.Int("users", 100, "number of distinct user IDs")
	messagesFlag := flag.Int("messages", 100000, "total messages per topic")
	topicUserFlag := flag.String("topic-user", "tasks/by_user", "user-partitioned topic path")
	topicIDFlag := flag.String("topic-id", "tasks/by_message_id", "message-ID-partitioned topic path")
	flag.Parse()

	// Configure structured JSON logging.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	// Validate flags.
	if *usersFlag < 1 {
		slog.Error("flag -users must be >= 1", "value", *usersFlag)
		os.Exit(1)
	}
	if *messagesFlag < 1 {
		slog.Error("flag -messages must be >= 1", "value", *messagesFlag)
		os.Exit(1)
	}

	// Validate required env vars.
	endpoint := os.Getenv("YDB_ENDPOINT")
	if endpoint == "" {
		slog.Error("YDB_ENDPOINT is not set")
		os.Exit(1)
	}
	saKeyFile := os.Getenv("YDB_SA_KEY_FILE")
	if saKeyFile == "" {
		slog.Error("YDB_SA_KEY_FILE is not set")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	db, err := ydb.Open(ctx, endpoint,
		yc.WithServiceAccountKeyFileCredentials(saKeyFile),
		yc.WithInternalCA(),
	)
	if err != nil {
		slog.Error("ydb.Open failed", "err", err)
		os.Exit(1)
	}
	defer db.Close(context.Background()) //nolint:errcheck

	topicUser := db.Name() + "/" + *topicUserFlag
	topicID := db.Name() + "/" + *topicIDFlag
	totalMessages := int64(*messagesFlag)

	// Generate messages.
	sampler := NewUserIDSampler(*usersFlag)
	producer := NewProducer(db)
	messages := producer.Generate(*messagesFlag, *usersFlag, sampler)

	// Publish to both topics.
	if err := producer.Publish(ctx, messages, topicUser, func(m BenchMessage) string {
		return m.UserID.String()
	}); err != nil {
		if ctx.Err() != nil {
			os.Exit(1)
		}
		slog.Error("publish to by_user failed", "err", err)
		os.Exit(1)
	}

	if err := producer.Publish(ctx, messages, topicID, func(m BenchMessage) string {
		return m.ID.String()
	}); err != nil {
		if ctx.Err() != nil {
			os.Exit(1)
		}
		slog.Error("publish to by_message_id failed", "err", err)
		os.Exit(1)
	}

	if ctx.Err() != nil {
		os.Exit(1)
	}

	consumer := NewConsumer(db)
	var results []ScenarioResult

	// Scenario 1: by_user → stats (RMW, user-aligned — low TLI expected)
	var tli1 atomic.Int64
	r1, err := consumer.RunScenario(ctx,
		"by_user \u2192 stats",
		topicUser, "bench-byuser-stats",
		10, totalMessages, &tli1,
		statsWorkload(db, &tli1),
	)
	if ctx.Err() != nil {
		os.Exit(1)
	}
	if err != nil {
		slog.Error("scenario 1 failed", "err", err)
		os.Exit(1)
	}
	verifyStatsSum(ctx, db, totalMessages, "by_user \u2192 stats")
	resetStats(ctx, db)
	results = append(results, r1)

	// Scenario 2: by_user → processed (insert-only — zero TLI expected)
	r2, err := consumer.RunScenario(ctx,
		"by_user \u2192 processed",
		topicUser, "bench-byuser-processed",
		10, totalMessages, nil,
		processedWorkload(db),
	)
	if ctx.Err() != nil {
		os.Exit(1)
	}
	if err != nil {
		slog.Error("scenario 2 failed", "err", err)
		os.Exit(1)
	}
	results = append(results, r2)

	// Scenario 3: by_message_id → stats (RMW, random partitioning — high TLI expected)
	var tli3 atomic.Int64
	r3, err := consumer.RunScenario(ctx,
		"by_message_id \u2192 stats",
		topicID, "bench-bymsgid-stats",
		10, totalMessages, &tli3,
		statsWorkload(db, &tli3),
	)
	if ctx.Err() != nil {
		os.Exit(1)
	}
	if err != nil {
		slog.Error("scenario 3 failed", "err", err)
		os.Exit(1)
	}
	verifyStatsSum(ctx, db, totalMessages, "by_message_id \u2192 stats")
	resetStats(ctx, db)
	results = append(results, r3)

	// Scenario 4: by_message_id → processed (insert-only — zero TLI expected)
	r4, err := consumer.RunScenario(ctx,
		"by_message_id \u2192 processed",
		topicID, "bench-bymsgid-processed",
		10, totalMessages, nil,
		processedWorkload(db),
	)
	if ctx.Err() != nil {
		os.Exit(1)
	}
	if err != nil {
		slog.Error("scenario 4 failed", "err", err)
		os.Exit(1)
	}
	results = append(results, r4)

	if ctx.Err() != nil {
		// Graceful shutdown: do not print the table.
		os.Exit(1)
	}

	printTable(results)
}

// verifyStatsSum queries SUM(a)+SUM(b)+SUM(c) and warns if it differs from expected.
func verifyStatsSum(ctx context.Context, db *ydb.Driver, expected int64, scenario string) {
	row, err := db.Query().QueryRow(ctx,
		`SELECT SUM(a) + SUM(b) + SUM(c) AS total FROM stats`,
	)
	if err != nil {
		slog.Warn("stats sum query failed", "scenario", scenario, "err", err)
		return
	}
	var total *int64
	if err := row.ScanNamed(query.Named("total", &total)); err != nil {
		slog.Warn("stats sum scan failed", "scenario", scenario, "err", err)
		return
	}
	actual := int64(0)
	if total != nil {
		actual = *total
	}
	if actual != expected {
		slog.Warn("stats sum mismatch", "scenario", scenario, "expected", expected, "actual", actual)
	}
}

// resetStats truncates the stats table between scenarios.
func resetStats(ctx context.Context, db *ydb.Driver) {
	if err := db.Query().Exec(ctx, `DELETE FROM stats`); err != nil {
		slog.Warn("DELETE FROM stats failed", "err", err)
	}
}

// printTable writes the Unicode box-drawing comparison table to stdout.
func printTable(results []ScenarioResult) {
	const (
		scenarioW  = 30
		messagesW  = 10
		tliW       = 12
		durationW  = 10
		msgPerSecW = 9
	)

	sep := func(left, mid, right, fill string, widths ...int) string {
		parts := make([]string, len(widths))
		for i, w := range widths {
			parts[i] = strings.Repeat(fill, w)
		}
		result := left
		for i, p := range parts {
			if i > 0 {
				result += mid
			}
			result += p
		}
		return result + right
	}

	row := func(cols ...string) string {
		widths := []int{scenarioW - 1, messagesW - 1, tliW - 1, durationW - 1, msgPerSecW - 1}
		s := "│"
		for i, col := range cols {
			w := widths[i]
			cell := " " + col
			if len(cell) > w {
				cell = cell[:w]
			}
			s += fmt.Sprintf("%-*s│", w, cell)
		}
		return s
	}

	top := sep("┌", "┬", "┐", "─", scenarioW, messagesW, tliW, durationW, msgPerSecW)
	mid := sep("├", "┼", "┤", "─", scenarioW, messagesW, tliW, durationW, msgPerSecW)
	bot := sep("└", "┴", "┘", "─", scenarioW, messagesW, tliW, durationW, msgPerSecW)

	fmt.Println(top)
	fmt.Println(row("Scenario", "Messages", "TLI Errors", "Duration", "msg/sec"))
	fmt.Println(mid)
	for _, r := range results {
		fmt.Println(row(
			r.Name,
			fmt.Sprintf("%d", r.Messages),
			fmt.Sprintf("%d", r.TLIErrors),
			formatDuration(r.Duration),
			fmt.Sprintf("%.0f", r.MsgPerSec),
		))
	}
	fmt.Println(bot)
}

// formatDuration formats a duration as e.g. "45.2s".
func formatDuration(d interface{ Seconds() float64 }) string {
	return fmt.Sprintf("%.1fs", d.Seconds())
}
