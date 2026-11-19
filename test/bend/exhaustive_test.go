//go:build bendrules

package bend

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The exhaustive cases. A random scenario is a sample; these are not. Each
// test takes a short list of steps (its alphabet) and runs every sequence
// of them up to a length, on the node and on the model. Within that bound
// nothing is left to chance: if the node and the model can disagree on
// some order of these steps of that length, the test finds it.
//
//   BEND_DEPTH=4            the length; unset or 0 skips the tests
//   BEND_SHARD=1/4          run every fourth sequence, starting with the
//                           second: for several processes side by side
//
// The two lock tests go one step deeper than BEND_DEPTH, because their
// alphabet is small and a lock needs more steps to run out.

func depth(t *testing.T, extra int) int {
	d, _ := strconv.Atoi(os.Getenv("BEND_DEPTH"))
	if d <= 0 {
		t.Skip("BEND_DEPTH is not set")
	}
	return d + extra
}

// sequences lists every sequence of 1 to depth steps out of n, as indexes,
// and keeps the ones of this process's shard.
func sequences(t *testing.T, n, depth int) [][]int {
	shard, of := 0, 1
	if v := os.Getenv("BEND_SHARD"); v != "" {
		if _, err := fmt.Sscanf(v, "%d/%d", &shard, &of); err != nil || of < 1 || shard < 0 || shard >= of {
			t.Fatalf("BEND_SHARD=%q: want i/n with 0 <= i < n", v)
		}
	}
	var out [][]int
	count := 0
	for length := 1; length <= depth; length++ {
		seq := make([]int, length)
		for {
			if count%of == shard {
				out = append(out, append([]int(nil), seq...))
			}
			count++
			i := length - 1
			for i >= 0 {
				seq[i]++
				if seq[i] < n {
					break
				}
				seq[i] = 0
				i--
			}
			if i < 0 {
				break
			}
		}
	}
	return out
}

func seqName(seq []int) string {
	parts := make([]string, len(seq))
	for i, v := range seq {
		parts[i] = strconv.Itoa(v)
	}
	return "x-" + strings.Join(parts, ".")
}

// The model runs this many scenarios per container. Bend's runtime
// overflows its stack on a much larger generated program (about 1,500
// five-step scenarios did).
const chunk = 400

// TestExhaustiveLedger is case BX-1.
func TestExhaustiveLedger(t *testing.T) {
	d := depth(t, 0)
	// A send at a height that is taken is only tried when a momentum has
	// confirmed the account's blocks; otherwise the step leaves a gap. (At a
	// pending height the pool may replace the block, which the model does
	// not describe; TestReplacementSpendsOnce covers that.)
	taken := func(n *node, dirty map[int]bool) op {
		o := send(0, 1, exactly(7))(n, dirty)
		if !dirty[0] && o.height > 1 {
			o.height--
		} else {
			o.height++
		}
		return o
	}
	alphabet := []step{
		send(0, 1, exactly(7)), send(0, 1, all), send(0, 1, allPlus(1)), send(1, 0, all),
		receive(1, 0), receive(1, 1), receive(0, 0), receive(0, 1), receive(2, 0),
		tick(), taken,
	}
	seqs := sequences(t, len(alphabet), d)
	different := 0
	for start := 0; start < len(seqs); start += chunk {
		end := min(start+chunk, len(seqs))
		var results []result
		for _, seq := range seqs[start:end] {
			steps := make([]step, len(seq))
			for i, v := range seq {
				steps[i] = alphabet[v]
			}
			results = append(results, runScenario(t, seqName(seq), steps))
		}
		lines := model(t, results)
		for i, r := range results {
			if lines[i] != r.nodeLine {
				different++
				t.Errorf("%s: the node and the model disagree\n  node:  %s\n  model: %s", r.name, r.nodeLine, lines[i])
				for j, o := range r.ops {
					t.Logf("  %2d %v %s", j, o, r.reasons[j])
				}
			}
		}
	}
	t.Logf("every sequence of up to %d of %d steps: %d sequences, %d different", d, len(alphabet), len(seqs), different)
}

func exhaustiveEscrow(t *testing.T, ptlc bool, id string) {
	d := depth(t, 0)
	alphabet := []escrowStep{
		create(0, 1, exactly(500), 100), create(0, 1, exactly(500), 30),
		unlock(1, 0, true), unlock(2, 0, false), unlock(1, 1, true),
		reclaim(0, 0), reclaim(2, 0),
		wait(), refused(0),
	}
	seqs := sequences(t, len(alphabet), d)
	different := 0
	for start := 0; start < len(seqs); start += chunk {
		end := min(start+chunk, len(seqs))
		var results []escrowResult
		for _, seq := range seqs[start:end] {
			steps := make([]escrowStep, len(seq))
			for i, v := range seq {
				steps[i] = alphabet[v]
			}
			results = append(results, runEscrow(t, ptlc, seqName(seq), steps))
		}
		var src strings.Builder
		src.WriteString(escrowHeader)
		for _, r := range results {
			calls := make([]string, len(r.calls))
			for i, c := range r.calls {
				calls[i] = c.bend()
			}
			fmt.Fprintf(&src, "    IO.print(T.Trace.run([%s], E.Vault{%s, [], %s, 0n}))\n", strings.Join(calls, ", "), natList(r.bals), nat(uint64(r.now)))
		}
		lines := runBend(t, "escrow-"+id, src.String(), len(results))
		for i, r := range results {
			if lines[i] != r.nodeLine {
				different++
				t.Errorf("%s: the node and the model disagree\n  node:  %s\n  model: %s", r.name, r.nodeLine, lines[i])
				for j, c := range r.asked {
					t.Logf("  %2d %v", j, c)
				}
			}
		}
	}
	t.Logf("every sequence of up to %d of %d steps: %d sequences, %d different", d, len(alphabet), len(seqs), different)
}

// TestExhaustiveHtlc is case BX-2.
func TestExhaustiveHtlc(t *testing.T) { exhaustiveEscrow(t, false, "BX-2") }

// TestExhaustivePtlc is case BX-3.
func TestExhaustivePtlc(t *testing.T) { exhaustiveEscrow(t, true, "BX-3") }

func exhaustiveLock(t *testing.T, stake bool, id string) {
	d := depth(t, 1)
	alphabet := []lockStep{
		lock(0, exactly(50), 1), lock(1, exactly(50), 3),
		cancel(0, 0), cancel(1, 0), cancel(1, 1),
		pause(),
	}
	seqs := sequences(t, len(alphabet), d)
	different := 0
	for start := 0; start < len(seqs); start += chunk {
		end := min(start+chunk, len(seqs))
		var results []lockResult
		for _, seq := range seqs[start:end] {
			steps := make([]lockStep, len(seq))
			for i, v := range seq {
				steps[i] = alphabet[v]
			}
			results = append(results, runLock(t, stake, seqName(seq), steps))
		}
		var src strings.Builder
		src.WriteString(escrowHeader)
		for _, r := range results {
			calls := make([]string, len(r.calls))
			for i, c := range r.calls {
				calls[i] = c.bend()
			}
			fmt.Fprintf(&src, "    IO.print(T.Trace.run([%s], E.Vault{%s, [], %s, 0n}))\n", strings.Join(calls, ", "), natList(r.bals), nat(uint64(r.now)))
		}
		lines := runBend(t, "escrow-"+id, src.String(), len(results))
		for i, r := range results {
			if lines[i] != r.nodeLine {
				different++
				t.Errorf("%s: the node and the model disagree\n  node:  %s\n  model: %s", r.name, r.nodeLine, lines[i])
				for j, c := range r.asked {
					t.Logf("  %2d %v  %s", j, c, r.notes[j])
				}
			}
		}
	}
	t.Logf("every sequence of up to %d of %d steps: %d sequences, %d different", d, len(alphabet), len(seqs), different)
}

// TestExhaustiveStake is case BX-4.
func TestExhaustiveStake(t *testing.T) { exhaustiveLock(t, true, "BX-4") }

// TestExhaustiveFusion is case BX-5.
func TestExhaustiveFusion(t *testing.T) { exhaustiveLock(t, false, "BX-5") }

const escrowHeader = "import Base\nimport ./escrow/escrow.bend as E\nimport ./escrow/trace.bend as T\n\ndef main() -> IO(Unit):\n  do IO<Unit>:\n"
