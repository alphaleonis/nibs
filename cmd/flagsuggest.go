package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// flagSuggestionMaxDist bounds the Levenshtein distance for "Did you mean" hints.
const flagSuggestionMaxDist = 2

// installFlagSuggestions wires "Did you mean --foo?" hints onto rootCmd.
// Subcommands inherit via Cobra's FlagErrorFunc() which walks up to the
// parent when a command has no flagErrorFunc of its own (see Cobra v1.10.2
// command.go:547-558). This means root-only registration covers every
// subcommand regardless of init order.
func installFlagSuggestions(root *cobra.Command) {
	root.SetFlagErrorFunc(suggestFlagOnError)
}

// suggestFlagOnError is Cobra's FlagErrorFunc — appends a hint for unknown long flags.
func suggestFlagOnError(cmd *cobra.Command, err error) error {
	const prefix = "unknown flag: --"
	msg := err.Error()
	if !strings.HasPrefix(msg, prefix) {
		return err
	}
	name := strings.TrimPrefix(msg, prefix)
	candidates := collectFlagNames(cmd)
	suggestion, ok := findClosestFlag(name, candidates, flagSuggestionMaxDist)
	if !ok {
		return err
	}
	return fmt.Errorf("%s\nDid you mean --%s?", msg, suggestion)
}

// collectFlagNames returns long-flag names visible to cmd (local + inherited persistent).
func collectFlagNames(cmd *cobra.Command) []string {
	var names []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		names = append(names, f.Name)
	})
	return names
}

// findClosestFlag returns the candidate closest to unknown by Levenshtein
// distance, if there is a unique winner within maxDist. Returns ("", false)
// when no candidate is close enough, or when multiple candidates tie at the
// minimum distance.
func findClosestFlag(unknown string, candidates []string, maxDist int) (string, bool) {
	best := ""
	bestDist := maxDist + 1
	tie := false
	for _, c := range candidates {
		d := levenshtein(unknown, c)
		if d < bestDist {
			bestDist = d
			best = c
			tie = false
		} else if d == bestDist {
			tie = true
		}
	}
	if best == "" || bestDist > maxDist || tie {
		return "", false
	}
	return best, true
}

// levenshtein computes the edit distance between a and b.
func levenshtein(a, b string) int {
	ar := []rune(a)
	br := []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			curr[j] = min(del, ins, sub)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}
