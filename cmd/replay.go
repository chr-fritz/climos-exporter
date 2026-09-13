/*
 * Copyright © 2023-2026 Christian Fritz <mail@chr-fritz.de>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/chr-fritz/climos-exporter/pkg/climos"
	"github.com/spf13/cobra"
)

const (
	changesFlag    = "changes"
	registerFlag   = "register"
	maxChangesFlag = "max-changes"
	startFlag      = "start"
	againstFlag    = "against"
)

// ReplayOptions holds what the replay command was asked to show.
type ReplayOptions struct {
	changes    bool
	registers  []string
	maxChanges int
	start      string
	against    string
}

func NewReplayOptions() *ReplayOptions {
	return &ReplayOptions{}
}

func NewReplayCommand() *cobra.Command {
	options := NewReplayOptions()

	cmd := cobra.Command{
		Use:   "replay <dump>",
		Short: "Decode a recorded bytestream and report what the registers did",
		Long: `Replays a raw bytestream recorded with --stream-dir through the same framer
and register decoder the exporter uses, without touching a serial port.

Without a flag it lists every register the recording carried, the ones that
moved most first. --changes lists the individual value changes in the order
they happened, dropping the registers that change constantly, which is what
makes a setting changed at the panel stand out from the temperatures.

--against compares two recordings. A settings register reaches the bus when it
changes and again in the register dump after a restart, and at no other time, so
a recording holds the complete configuration only if it spans a restart.
Comparing two such recordings is what identifies which register carries which
setting.

Times are counted from the filter countdown, the only monotone clock on the bus.
It advances one minute per running minute and stands still while the unit is
off, so it measures running time rather than wall clock time.`,
		Args: cobra.ExactArgs(1),
		RunE: options.run,
	}

	cmd.Flags().BoolVar(&options.changes, changesFlag, false, "List the individual register changes in recording order instead of the per register summary.")
	cmd.Flags().StringSliceVar(&options.registers, registerFlag, nil, "Restrict the report to these registers, hexadecimal, e.g. 0x44,0x55.")
	cmd.Flags().IntVar(&options.maxChanges, maxChangesFlag, 50, "With --changes, skip registers that change more often than this over the whole recording. 0 keeps all of them.")
	cmd.Flags().StringVar(&options.start, startFlag, "", "Wall clock time of the first byte, as 15:04 or a full RFC3339 timestamp, to print absolute times next to the running time.")
	cmd.Flags().StringVar(&options.against, againstFlag, "", "Compare against this second recording and report the registers whose value differs.")
	_ = cmd.RegisterFlagCompletionFunc(againstFlag, func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"bin"}, cobra.ShellCompDirectiveFilterFileExt
	})

	return &cmd
}

func (o *ReplayOptions) run(cmd *cobra.Command, args []string) error {
	selected, err := parseRegisters(o.registers)
	if err != nil {
		return err
	}
	start, err := parseStart(o.start)
	if err != nil {
		return err
	}

	analyzer, stats, err := analyze(args[0])
	if err != nil {
		return err
	}

	// Every report below writes through a tabwriter onto this same writer and
	// ends in a Flush whose error is returned, so a broken output is reported
	// there rather than at each individual write.
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "frames=%d repaired=%d (%.1f%%) data=%d undecodable=%d\n\n",
		stats.Frames, stats.Repaired, repairedShare(stats), stats.DataFrames, stats.Undecodable)

	switch {
	case o.against != "":
		return o.writeComparison(out, analyzer, selected)
	case o.changes:
		return o.writeChanges(out, analyzer, selected, start)
	default:
		return writeProfiles(out, analyzer, selected)
	}
}

func analyze(path string) (*climos.Analyzer, climos.ReplayStats, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, climos.ReplayStats{}, err
	}

	analyzer := climos.NewAnalyzer()
	return analyzer, climos.Replay(data, analyzer.Add), nil
}

// writeComparison reports the registers that hold a different value in the two
// recordings. Registers that move on their own inside either recording are left
// out: a temperature differs between any two days and says nothing.
func (o *ReplayOptions) writeComparison(out io.Writer, analyzer *climos.Analyzer, selected map[climos.Register]bool) error {
	other, _, err := analyze(o.against)
	if err != nil {
		return err
	}

	writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "REGISTER\tTHIS\tAGAINST")

	before := lastValues(other)
	for _, profile := range stableProfiles(analyzer, o.maxChanges) {
		if !wanted(selected, profile.Register) {
			continue
		}
		was, seen := before[profile.Register]
		if !seen || bytes.Equal(was, profile.Last) {
			continue
		}
		_, _ = fmt.Fprintf(writer, "%s\t%x\t%x\n", profile.Register, profile.Last, was)
	}
	return writer.Flush()
}

// stableProfiles keeps the registers that hold still within a recording, in
// register order rather than by activity: a comparison reads as a table of
// settings, and settings are looked up by id.
func stableProfiles(analyzer *climos.Analyzer, maxChanges int) []climos.RegisterProfile {
	profiles := analyzer.Profiles()
	stable := make([]climos.RegisterProfile, 0, len(profiles))
	for _, profile := range profiles {
		if maxChanges > 0 && profile.Changes > maxChanges {
			continue
		}
		stable = append(stable, profile)
	}
	sort.Slice(stable, func(i, j int) bool { return stable[i].Register < stable[j].Register })
	return stable
}

func lastValues(analyzer *climos.Analyzer) map[climos.Register][]byte {
	out := map[climos.Register][]byte{}
	for _, profile := range analyzer.Profiles() {
		out[profile.Register] = profile.Last
	}
	return out
}

func repairedShare(stats climos.ReplayStats) float64 {
	if stats.Frames == 0 {
		return 0
	}
	return 100 * float64(stats.Repaired) / float64(stats.Frames)
}

func writeProfiles(out io.Writer, analyzer *climos.Analyzer, selected map[climos.Register]bool) error {
	writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "REGISTER\tFRAMES\tCHANGES\tDISTINCT\tFIRST\tLAST")

	for _, profile := range analyzer.Profiles() {
		if !wanted(selected, profile.Register) {
			continue
		}
		_, _ = fmt.Fprintf(writer, "%s\t%d\t%d\t%d\t%x\t%x\n",
			profile.Register, profile.Frames, profile.Changes, profile.Distinct, profile.First, profile.Last)
	}
	return writer.Flush()
}

func (o *ReplayOptions) writeChanges(out io.Writer, analyzer *climos.Analyzer, selected map[climos.Register]bool, start time.Time) error {
	counts := analyzer.ChangeCounts()

	writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, header(start))

	for _, change := range analyzer.Changes() {
		if !wanted(selected, change.Register) {
			continue
		}
		if o.tooNoisy(counts[change.Register], selected) {
			continue
		}
		_, _ = fmt.Fprintf(writer, "%s\t%d\t%s\t%x\t%x\n",
			runningTime(change.Elapsed, start), change.Offset, change.Register, change.From, change.To)
	}
	return writer.Flush()
}

// tooNoisy drops the registers that move all the time. An explicit --register
// selection overrides it: asking for a register by name means wanting to see it
// however often it moves.
func (o *ReplayOptions) tooNoisy(changes int, selected map[climos.Register]bool) bool {
	if len(selected) > 0 || o.maxChanges <= 0 {
		return false
	}
	return changes > o.maxChanges
}

func header(start time.Time) string {
	if start.IsZero() {
		return "RUNNING\tOFFSET\tREGISTER\tFROM\tTO"
	}
	return "RUNNING/CLOCK\tOFFSET\tREGISTER\tFROM\tTO"
}

func runningTime(elapsed time.Duration, start time.Time) string {
	running := fmt.Sprintf("+%02d:%02d", int(elapsed.Hours()), int(elapsed.Minutes())%60)
	if start.IsZero() {
		return running
	}
	return running + " " + start.Add(elapsed).Format("15:04")
}

func wanted(selected map[climos.Register]bool, register climos.Register) bool {
	return len(selected) == 0 || selected[register]
}

// parseRegisters reads the register ids the way docs/protocol.md writes them,
// which is hexadecimal with or without the 0x prefix. Decimal is not accepted:
// "44" meaning 0x2c would silently report the wrong register.
func parseRegisters(values []string) (map[climos.Register]bool, error) {
	selected := make(map[climos.Register]bool, len(values))
	for _, value := range values {
		trimmed := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(value)), "0x")
		id, err := strconv.ParseUint(trimmed, 16, 16)
		if err != nil {
			return nil, fmt.Errorf("%q is not a hexadecimal register id: %w", value, err)
		}
		selected[climos.Register(id)] = true
	}
	return selected, nil
}

func parseStart(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	if start, err := time.Parse(time.RFC3339, value); err == nil {
		return start, nil
	}
	start, err := time.Parse("15:04", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is neither 15:04 nor RFC3339", value)
	}
	return start, nil
}

func init() {
	rootCmd.AddCommand(NewReplayCommand())
}
