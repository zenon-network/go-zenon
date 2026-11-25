//go:build e2e

package e2e

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
)

// TestVerifierAccountRules covers the account-block verifier rules that the
// B-series in ledger_test.go does not reach: send-side amount fields, the
// momentum-acknowledged gap check, the Dynamic-Plasma-gated receive-data
// rule, the transaction-level plasma/descendant/public-key rules, and the
// embedded-receive sequencer order. Every case publishes a block a correct
// client would never build and expects the pool to refuse it with the
// verifier's own message; refusals leave no trace on the chain.
//
// The sequencer and plasma-fields cases race the pillar that
// auto-generates contract receives. Measured on the devnet: sends
// confirm in momentum N and sit unreceived until the auto-receives
// commit in momentum N+1, but the pool's sequencer front advances
// earlier, as the auto-receive blocks arrive over gossip inside that
// window. The cases forge receives in tight retry loops and publish a
// fresh set of sends only when the sequencer drained before the
// expected refusal.
func TestVerifierAccountRules(t *testing.T) {
	group(t)
	a := newAccount(t, 130, 50, 800, 1000)
	b := newAccount(t, 131, 10, 500, 1000)

	// momentumAt returns the momentum hash at exactly the given height.
	momentumAt := func(t *testing.T, height uint64) types.HashHeight {
		t.Helper()
		var list struct {
			List []struct {
				Hash   types.Hash `json:"hash"`
				Height uint64     `json:"height"`
			} `json:"list"`
		}
		get(t, &list, "ledger.getMomentumsByHeight", height, 1)
		if len(list.List) != 1 || list.List[0].Height != height {
			t.Fatalf("momentum at height %d: got %+v, want exactly that height", height, list.List)
		}
		return types.HashHeight{Hash: list.List[0].Hash, Height: height}
	}

	// confirmationHeight returns the momentum height that confirmed the block.
	confirmationHeight := func(t *testing.T, hash types.Hash) types.HashHeight {
		t.Helper()
		var blk *api.AccountBlock
		get(t, &blk, "ledger.getAccountBlockByHash", hash)
		if blk == nil || blk.ConfirmationDetail == nil {
			t.Fatalf("block %s is not confirmed", hash)
		}
		return types.HashHeight{Hash: blk.ConfirmationDetail.MomentumHash, Height: blk.ConfirmationDetail.MomentumHeight}
	}

	t.Run("B-10_send_amount_fields_refused", func(t *testing.T) {
		cover(t, "E6", "error", "verifier.ErrABAmountNegative", "verifier.ErrABZtsMissing", "verifier.ErrABFromBlockHashMustBeZero")
		cover(t, "E6", "verifier-rule", "accountBlockVerifier.amounts")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Fields: func(blk *nom.AccountBlock) {
			blk.Amount = new(big.Int).Neg(units(1))
		}}), "amount can't be negative")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Fields: func(blk *nom.AccountBlock) {
			blk.TokenStandard = types.ZeroTokenStandard
		}}), "zts is missing")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Fields: func(blk *nom.AccountBlock) {
			blk.FromBlockHash = types.NewHash([]byte("not zero " + runTag))
		}}), "from-block-hash must be zero")
	})

	t.Run("B-11_acknowledgement_gap_refused", func(t *testing.T) {
		cover(t, "E6", "error", "verifier.ErrABMAGap")
		cover(t, "E6", "verifier-rule", "accountBlockVerifier.momentumAcknowledged")
		// The acknowledged momentum must not be older than the one the
		// previous block of this account acknowledged — that is the previous
		// block's own MomentumAcknowledged, not the height of the momentum
		// that confirmed it (confirmation can lag acknowledgement).
		var frontier *api.AccountBlock
		get(t, &frontier, "ledger.getFrontierAccountBlock", a.Address)
		if frontier == nil || frontier.ConfirmationDetail == nil {
			t.Fatalf("account %s has no confirmed frontier block", a.Address)
		}
		prevAck := frontier.MomentumAcknowledged.Height
		if prevAck < 2 {
			t.Skipf("account frontier acknowledged momentum %d, too early for a gap case", prevAck)
		}
		older := momentumAt(t, prevAck-1)
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Fields: func(blk *nom.AccountBlock) {
			blk.MomentumAcknowledged = older
		}}), "older momentum than previous")
	})

	t.Run("B-12_receive_with_data_refused", func(t *testing.T) {
		blk := transfer(t, a, b.Address, znn, units(3), nil)
		if _, err := net.WaitConfirmed(blk.Hash, wait); err != nil {
			t.Fatal(err)
		}
		withData := &Mutator{Fields: func(blk *nom.AccountBlock) {
			blk.Data = []byte{1}
		}}
		if !live["dynamic-plasma"] {
			// The rule is gated on Dynamic Plasma: before it, a receive may
			// carry data (it gets flat plasma regardless of size, which is
			// what the rule closes). Assert the off state instead of
			// skipping, so the case says something on every variant.
			recv, err := net.Receive(b, blk.Hash, withData)
			if err != nil {
				t.Fatalf("receive with data before dynamic plasma: %v (the rule must be off)", err)
			}
			if _, err := net.WaitConfirmed(recv.Hash, wait); err != nil {
				t.Fatal(err)
			}
			return
		}
		cover(t, "E7", "error", "verifier.ErrABDataMustBeEmpty")
		cover(t, "E7", "verifier-rule", "accountBlockVerifier.receiveData", "accountBlockVerifier.isDynamicPlasmaActiveAtMA")
		_, err := net.Receive(b, blk.Hash, withData)
		mustReject(t, err, "data must be empty on receive blocks")
		receiveAll(t, b, 1)
	})

	t.Run("B-13_forged_embedded_receive_refused", func(t *testing.T) {
		// Each refusal is credited on the child that exercises it, not here:
		// a -run selection of one child must not record the others' items.
		// forge publishes a fake contract receive for the plasma contract.
		// fromHash must be a confirmed, still-unreceived send to the contract,
		// and the acknowledged momentum must be the one that confirmed it,
		// or the earlier rules refuse first. tail mutates the signed block.
		forge := func(t *testing.T, kp *wallet.KeyPair, fromHash types.Hash, tail func(*nom.AccountBlock)) error {
			t.Helper()
			conf := confirmationHeight(t, fromHash)
			var contractFrontier *api.AccountBlock
			get(t, &contractFrontier, "ledger.getFrontierAccountBlock", types.PlasmaContract)
			height, prevHash := uint64(1), types.ZeroHash
			if contractFrontier != nil {
				height, prevHash = contractFrontier.Height+1, contractFrontier.Hash
			}
			_, err := net.Send(kp, &nom.AccountBlock{
				BlockType:     nom.BlockTypeContractReceive,
				Address:       types.PlasmaContract,
				FromBlockHash: fromHash,
			}, &Mutator{
				Fields: func(blk *nom.AccountBlock) {
					blk.Address = types.PlasmaContract
					blk.Height = height
					blk.PreviousHash = prevHash
					blk.MomentumAcknowledged = conf
				},
				Plasma: func(blk *nom.AccountBlock) { blk.FusedPlasma, blk.Difficulty = 0, 0 },
				Signed: func(blk *nom.AccountBlock) {
					blk.PublicKey, blk.Signature = nil, nil
					if tail != nil {
						tail(blk)
					}
				},
			})
			return err
		}

		// threeSends publishes three valid Fuse sends to the plasma
		// contract and waits for all to confirm in one momentum. The
		// committed mailbox then keeps all three unreceived until the
		// next momentum (~10 s) auto-receives them together — measured
		// with a mailbox probe, and a lone send has no observable window
		// at all. The pool's sequencer front, however, advances as the
		// auto-receives arrive over gossip, inside the confirmation
		// momentum period; the forgeries below ride that pool-level race.
		threeSends := func(t *testing.T) (types.Hash, types.Hash, types.Hash) {
			t.Helper()
			fuse := definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, a.Address)
			b1 := transfer(t, a, types.PlasmaContract, qsr, coins(10), fuse)
			b2 := transfer(t, a, types.PlasmaContract, qsr, coins(10), fuse)
			b3 := transfer(t, b, types.PlasmaContract, qsr, coins(10), fuse)
			// Poll fast: the pre-pop window for forging the first send
			// opens at confirmation and ends when the first auto-receive
			// gossip arrives, so shave the 1 s WaitConfirmed poll interval.
			deadline := time.Now().Add(wait)
			for {
				done := true
				for _, blk := range []*nom.AccountBlock{b1, b2, b3} {
					cb, err := net.Block(0, blk.Hash)
					if err != nil || cb == nil || cb.ConfirmationDetail == nil {
						done = false
					}
				}
				if done {
					return b1.Hash, b2.Hash, b3.Hash
				}
				if time.Now().After(deadline) {
					t.Fatal("three sends not confirmed in time")
				}
				time.Sleep(50 * time.Millisecond)
			}
		}

		// raceForgery publishes a fresh set of three sends and forges
		// receives of the target (1 = second, 2 = third) until the
		// verifier refuses with want, and returns the target send's hash.
		// The committed mailbox keeps all three unreceived until the
		// momentum after confirmation, but the pool's sequencer front
		// advances as the auto-receives arrive over gossip — racy within a
		// second — so the forge retries against the same send set until the
		// pool state matches the case. Reaching the VM (changes-hash
		// refusal) means the pool's front had already advanced past the
		// send, so the forge was next in line; the case retries with a
		// fresh send set instead of treating it as a verdict on the
		// sequencer rule.
		raceForgery := func(t *testing.T, want string, target int, tail func(*nom.AccountBlock)) types.Hash {
			t.Helper()
			deadline := time.Now().Add(wait)
		attempts:
			for {
				_, second, third := threeSends(t)
				hash := second
				if target == 2 {
					hash = third
				}
				inner := time.Now().Add(45 * time.Second)
				for {
					err := forge(t, a, hash, tail)
					switch {
					case err == nil:
						t.Fatalf("forged receive (%q case) was accepted", want)
					case contains(err, want):
						return hash
					case contains(err, "changes-hash"):
						// The pool's sequencer front advanced past this
						// send before the forge arrived (the auto-receives
						// enter the pool with the confirming momentum), so
						// this block was next in line and the VM rightly
						// refused the stale forge. Not a verdict on the
						// sequencer rule: try a fresh send set. A sequencer
						// that never refuses slips every attempt and fails
						// by the deadline below.
						t.Logf("attempt lost to the changes-hash wall (front already past the target): %v", err)
						continue attempts
					case contains(err, "Not next in line"):
						// an earlier send is still front; wait for the pops
						t.Logf("front still behind the target, refiring: %v", err)
					case contains(err, "already received") || contains(err, "Nothing to receive") || contains(err, "cemented"):
						// The sequencer drained, or a momentum committed the real
						// receives and the recorded predecessor fell below the
						// stable account height (the cemented refusal): the pool
						// state this attempt raced is gone, so try a fresh send
						// set rather than reading a verdict into a timing outcome.
						t.Logf("attempt lost, pool state gone: %v", err)
						continue attempts
					default:
						t.Fatalf("refused with %q, want %q", err, want)
					}
					if time.Now().After(inner) {
						t.Fatalf("no %q refusal within the inner window", want)
					}
					time.Sleep(100 * time.Millisecond)
					if time.Now().After(deadline) {
						t.Fatalf("no %q refusal within the deadline", want)
					}
				}
			}
		}

		t.Run("B-13a_not_next_in_line_refused", func(t *testing.T) {
			cover(t, "E6", "error", "verifier.ErrABSequencerNotNext")
			cover(t, "E6", "verifier-rule", "accountBlockVerifier.sequencer")
			target := raceForgery(t, "Not next in line", 2, nil)
			// The race sends are followed by auto-generated contract
			// receives that pass the sequencer and the contract-receive
			// rules on every node that inserts them — the happy path this
			// race depends on. Credit that positive coverage only after
			// the target's receive is committed: the committed mailbox no
			// longer lists the send (the driver's received map only tracks
			// receives it published itself, so it cannot see the pillars'
			// auto-receives). The refusal above alone does not show a
			// valid receive passed the rules.
			deadline := time.Now().Add(wait)
			for {
				un, err := net.Unreceived(types.PlasmaContract)
				if err != nil {
					t.Fatalf("unreceived sends of the plasma contract: %v", err)
				}
				pending := false
				for _, u := range un {
					if u.Hash == target {
						pending = true
						break
					}
				}
				if !pending {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("the auto-generated receive of %s not committed within %v", target, wait)
				}
				time.Sleep(500 * time.Millisecond)
			}
			cover(t, "E1", "verifier-rule", "accountBlockTransactionVerifier.contractReceiveRulesActive")
		})

		// The forged receive of the actual next send passes the block-level
		// rules and reaches the transaction-level plasma and signature rules:
		// a contract receive must carry zero base/total plasma and no public
		// key (both excluded from the hash, so the honest hash still checks
		// out — that binding is what these rules add).
		// publishOne puts a single valid Fuse send into the pool and
		// returns its hash. The plasma-fields and public-key cases forge
		// the FIRST send, whose pool-level pre-pop window opens at its
		// confirmation and ends when the producer's auto-receives arrive
		// over gossip — measured at well under two seconds.
		publishOne := func(t *testing.T, from *wallet.KeyPair) types.Hash {
			t.Helper()
			fuse := definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, a.Address)
			return transfer(t, from, types.PlasmaContract, qsr, coins(10), fuse).Hash
		}

		// prepareForge pre-builds a forged contract receive for fromHash:
		// every field except the acknowledging momentum, which only
		// exists once the send confirms. The contract frontier is read
		// now; it stays valid until the contract's own pool blocks move,
		// which is exactly the arrival of the auto-receives the forge
		// races.
		prepareForge := func(t *testing.T, fromHash types.Hash, tail func(*nom.AccountBlock)) *nom.AccountBlock {
			t.Helper()
			var frontier *api.AccountBlock
			get(t, &frontier, "ledger.getFrontierAccountBlock", types.PlasmaContract)
			height, prevHash := uint64(1), types.ZeroHash
			if frontier != nil {
				height, prevHash = frontier.Height+1, frontier.Hash
			}
			blk := &nom.AccountBlock{
				Version:         1,
				ChainIdentifier: net.ChainID,
				BlockType:       nom.BlockTypeContractReceive,
				Address:         types.PlasmaContract,
				FromBlockHash:   fromHash,
				Height:          height,
				PreviousHash:    prevHash,
				Amount:          new(big.Int),
				TokenStandard:   types.ZeroTokenStandard,
			}
			if tail != nil {
				tail(blk)
			}
			return blk
		}

		// fireForge patches the acknowledgement, hashes and publishes
		// with a single RPC round trip, so the forge lands inside the
		// pre-pop window. Publishing directly bypasses sendBy's refusal
		// recording, so a refusal is recorded here: the end-of-run
		// no-trace check must see every refused hash.
		fireForge := func(planned *nom.AccountBlock, conf types.HashHeight) error {
			planned.MomentumAcknowledged = conf
			planned.Hash = planned.ComputeHash()
			err := net.Publish(planned)
			if err != nil {
				net.NoteRefused(planned, err)
			}
			return err
		}

		t.Run("B-13b_changes_hash_wall_refused", func(t *testing.T) {
			cover(t, "E6", "error", "vm/constants.ErrAutoReceiveChangesHashMismatch")
			// This case tests the VM changes-hash wall, not the
			// transaction-level plasmaFields rule: the forged receive
			// carries a lying BasePlasma (excluded from the block hash, so
			// the forgery rides an honest hash), but a contract receive is
			// re-generated by the VM on arrival and its changes-hash and
			// hash must match the regeneration — the refusal fires there,
			// before plasmaFields is reached. Removing the BasePlasma
			// mutation gives the same refusal. The plasmaFields rule's
			// contract-receive arm is only reachable by a forger who
			// executes the contract to compute a valid changes-hash — it
			// is covered by unit tests (vm/embedded/tests) instead.
			deadline := time.Now().Add(wait)
		attempts2:
			for {
				send := publishOne(t, a)
				planned := prepareForge(t, send, func(blk *nom.AccountBlock) { blk.BasePlasma = 1 })
				inner := time.Now().Add(45 * time.Second)
				for {
					cb, err := net.Block(0, send)
					if err == nil && cb != nil && cb.ConfirmationDetail != nil {
						conf := types.HashHeight{Hash: cb.ConfirmationDetail.MomentumHash, Height: cb.ConfirmationDetail.MomentumHeight}
						switch err := fireForge(planned, conf); {
						case err == nil:
							t.Fatal("forged receive with base plasma was accepted")
						case contains(err, "changes-hash"):
							return // the VM regeneration refused the forgery
						case contains(err, "already received") || contains(err, "Nothing to receive") || contains(err, "cemented"):
							// the auto-receives won; wait for the pool to
							// settle and try a fresh send
							time.Sleep(12 * time.Second)
							continue attempts2
						case contains(err, "Not next in line"):
							// stale sends still ahead in the mailbox; keep firing
						default:
							t.Fatalf("refused with %q, want the changes-hash wall", err)
						}
					}
					if time.Now().After(inner) {
						t.Fatal("no changes-hash refusal within the inner window")
					}
					time.Sleep(150 * time.Millisecond)
					if time.Now().After(deadline) {
						t.Fatal("no changes-hash refusal within the deadline")
					}
				}
			}
		})

		t.Run("B-13c_acknowledgement_must_match_confirmation", func(t *testing.T) {
			cover(t, "E6", "error", "verifier.ErrABMAInvalidForAutoGenerated")
			// An auto-generated receive must acknowledge the momentum that
			// confirmed its send; this forge acknowledges the momentum
			// before it. That is a block-level rule, refused before any
			// execution — no changes-hash needed, no wall to pass.
			deadline := time.Now().Add(wait)
		attempts3:
			for {
				send := publishOne(t, a)
				planned := prepareForge(t, send, nil)
				inner := time.Now().Add(45 * time.Second)
				for {
					cb, err := net.Block(0, send)
					if err == nil && cb != nil && cb.ConfirmationDetail != nil {
						conf := cb.ConfirmationDetail.MomentumHeight
						wrong := momentumAt(t, conf-1)
						switch err := fireForge(planned, wrong); {
						case err == nil:
							t.Fatal("forged receive with a stale acknowledgement was accepted")
						case contains(err, "invalid momentum for auto-generated"):
							return
						case contains(err, "already received") || contains(err, "Nothing to receive") || contains(err, "cemented"):
							time.Sleep(12 * time.Second)
							continue attempts3
						case contains(err, "Not next in line"):
						default:
							t.Fatalf("refused with %q, want the auto-generated-MA message", err)
						}
					}
					if time.Now().After(inner) {
						t.Fatal("no acknowledgement refusal within the inner window")
					}
					time.Sleep(150 * time.Millisecond)
					if time.Now().After(deadline) {
						t.Fatal("no acknowledgement refusal within the deadline")
					}
				}
			}
		})
	})

	t.Run("B-14_descendant_blocks_on_a_user_send_refused", func(t *testing.T) {
		cover(t, "E6", "error", "verifier.ErrABDescendantMustBeZero")
		cover(t, "E6", "verifier-rule", "accountBlockTransactionVerifier.descendantBlocks")
		// Reaching the descendant rule needs a well-formed batch:
		// block.Previous() reports the previous of the FIRST descendant,
		// so the child must chain onto the real account frontier
		// (child Height = frontier+1, child PreviousHash = frontier hash)
		// and the head must sit one above it with PreviousHash = child
		// hash. A malformed batch dies earlier, in getContext, with
		// "previous block is missing" — a probe with a debug build
		// confirmed the height-1 underflow that produces that refusal.
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Fields: func(blk *nom.AccountBlock) {
			fm, err := net.Frontier()
			if err != nil {
				t.Fatal(err)
			}
			var frontier *api.AccountBlock
			get(t, &frontier, "ledger.getFrontierAccountBlock", a.Address)
			if frontier == nil {
				t.Fatalf("account %s has no frontier block", a.Address)
			}
			ack := types.HashHeight{Hash: fm.Hash, Height: fm.Height}
			child := &nom.AccountBlock{
				Version:              1,
				ChainIdentifier:      net.ChainID,
				BlockType:            nom.BlockTypeUserSend,
				Address:              a.Address,
				ToAddress:            b.Address,
				Amount:               units(1),
				TokenStandard:        znn,
				Height:               frontier.Height + 1,
				PreviousHash:         frontier.Hash,
				MomentumAcknowledged: ack,
			}
			child.Hash = child.ComputeHash()
			blk.Height = frontier.Height + 2
			blk.PreviousHash = child.Hash
			blk.MomentumAcknowledged = ack
			blk.DescendantBlocks = []*nom.AccountBlock{child}
		}}), "descendant blocks must be empty")
	})
}

// TestVerifierMomentumRules covers the momentum-side verifier rules with
// positive assertions on the live chain: every momentum a node accepted
// passed these rules when it was inserted. The walk reads the last stretch
// of momentums and re-checks each rule's invariant from RPC data, and the
// dynamic-plasma boundary is checked one momentum below, at and above the
// enforcement height from history.
//
// What a live network cannot show without producer-side fault injection —
// a forged momentum reaching an honest node — is stated instead of covered:
// the negative arms of momentum.changesHash and momentum.contentByteBound,
// and the malformed-arms of the remaining rules.
func TestVerifierMomentumRules(t *testing.T) {
	group(t)

	dpEnf, dpKnown := sporkHeights["dynamic-plasma"]

	type momentum struct {
		Hash            types.Hash          `json:"hash"`
		Height          uint64              `json:"height"`
		Version         uint64              `json:"version"`
		ChainIdentifier uint64              `json:"chainIdentifier"`
		PreviousHash    types.Hash          `json:"previousHash"`
		Timestamp       int64               `json:"timestamp"`
		Data            []byte              `json:"data"`
		Content         nom.MomentumContent `json:"content"`
		NextFusionPrice uint64              `json:"nextFusionPrice"`
		NextWorkPrice   uint64              `json:"nextWorkPrice"`
		PublicKey       []byte              `json:"publicKey"`
	}
	read := func(t *testing.T, height uint64) momentum {
		t.Helper()
		var list struct {
			List []momentum `json:"list"`
		}
		get(t, &list, "ledger.getMomentumsByHeight", height, 1)
		if len(list.List) != 1 || list.List[0].Height != height {
			t.Fatalf("momentum at height %d: got %+v, want exactly that height", height, list.List)
		}
		return list.List[0]
	}

	t.Run("B-15_walk_the_recent_momentums", func(t *testing.T) {
		// momentumTransactionVerifier.producer is deliberately not credited:
		// the walk below checks the producer is a registered pillar, not
		// the pillar elected for that momentum's slot — no RPC answers the
		// per-slot election, so that arm is pinned by the verifier unit
		// tests instead.
		cover(t, "E1", "verifier-rule", "rawMomentumVerifier.all", "momentumVerifier.getContext", "rawMomentumVerifier.chainIdentifier",
			"rawMomentumVerifier.version", "rawMomentumVerifier.timestamp", "rawMomentumVerifier.previous", "rawMomentumVerifier.data",
			"rawMomentumVerifier.nextFusionPrice", "rawMomentumVerifier.nextWorkPrice", "rawMomentumVerifier.content")
		var pillars struct {
			List []struct {
				BlockProducingAddress types.Address `json:"producerAddress"`
			} `json:"list"`
		}
		get(t, &pillars, "embedded.pillar.getAll", 0, 200)
		producers := map[types.Address]bool{}
		for _, p := range pillars.List {
			producers[p.BlockProducingAddress] = true
		}
		if len(producers) == 0 {
			t.Fatal("no pillars on this chain")
		}
		frontier, err := net.Frontier()
		if err != nil {
			t.Fatal(err)
		}
		// Walk what the chain has: on a short chain (the driver can start
		// a few momentums after genesis) a shorter stretch still exercises
		// every rule, it just says less about history.
		stretch := uint64(30)
		if frontier.Height < stretch+1 {
			stretch = frontier.Height - 1
		}
		if stretch < 2 {
			t.Fatalf("chain too short to walk: height %d", frontier.Height)
		}
		var prev *momentum
		for h := frontier.Height - stretch + 1; h <= frontier.Height; h++ {
			m := read(t, h)
			if m.ChainIdentifier != net.ChainID {
				t.Errorf("momentum %d: chain identifier %d, want %d", h, m.ChainIdentifier, net.ChainID)
			}
			wantVersion := uint64(1)
			if dpKnown && h > dpEnf {
				wantVersion = 2
			}
			if m.Version != wantVersion {
				t.Errorf("momentum %d: version %d, want %d (dp enforced at %d, known %v)", h, m.Version, wantVersion, dpEnf, dpKnown)
			}
			if len(m.Data) != 0 {
				t.Errorf("momentum %d: data is %d bytes, want empty", h, len(m.Data))
			}
			if dpKnown && h > dpEnf {
				if m.NextFusionPrice < dp.MinResourcePrice || m.NextWorkPrice < dp.MinResourcePrice {
					t.Errorf("momentum %d: prices %d/%d, want both >= %d once dynamic plasma is in force", h, m.NextFusionPrice, m.NextWorkPrice, dp.MinResourcePrice)
				}
			} else {
				if m.NextFusionPrice != 0 || m.NextWorkPrice != 0 {
					t.Errorf("momentum %d: prices %d/%d, want both zero before dynamic plasma", h, m.NextFusionPrice, m.NextWorkPrice)
				}
			}
			if prev != nil {
				if m.PreviousHash != prev.Hash {
					t.Errorf("momentum %d: previous hash %s, want %s of momentum %d", h, m.PreviousHash, prev.Hash, prev.Height)
				}
				if m.Timestamp <= prev.Timestamp {
					t.Errorf("momentum %d: timestamp %d not increasing over %d", h, m.Timestamp, prev.Timestamp)
				}
			}
			if !producers[types.PubKeyToAddress(m.PublicKey)] && m.Height != 1 {
				t.Errorf("momentum %d: producer %s is not a registered pillar", h, types.PubKeyToAddress(m.PublicKey))
			}
			for _, entry := range m.Content {
				var blk *api.AccountBlock
				get(t, &blk, "ledger.getAccountBlockByHash", entry.Hash)
				if blk == nil || blk.ConfirmationDetail == nil || blk.ConfirmationDetail.MomentumHeight != m.Height {
					t.Errorf("momentum %d content entry %s does not resolve to a block confirmed by this momentum", h, entry.Hash)
				}
			}
			prev = &m
		}
	})

	t.Run("B-16_acknowledgement_recency_of_content_blocks", func(t *testing.T) {
		if !live["dynamic-plasma"] {
			// The rule is in force only once Dynamic Plasma is enforced at
			// the parent momentum. Assert the off state instead of skipping
			// or passing vacuously: a first block acknowledging a momentum
			// from before every spork is accepted. It must be a send — a
			// receive's acknowledgement is pinned to its send's confirmation
			// height by fromHash, and an established chain's by the gap rule,
			// so only a first send is free to acknowledge an old momentum.
			c := net.Key(runBase + 140)
			funder := net.Key(IdxFunder)
			callOK(t, funder, types.PlasmaContract, definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, c.Address), qsr, coins(2000))
			genesis := read(t, 1)
			err := tryTransfer(c, funder.Address, znn, units(0), &Mutator{Fields: func(blk *nom.AccountBlock) {
				blk.MomentumAcknowledged = types.HashHeight{Hash: genesis.Hash, Height: 1}
			}})
			if err != nil {
				t.Fatalf("first send acknowledging momentum 1 before dynamic plasma: %v (the recency rule must be off)", err)
			}
			return
		}
		cover(t, "E1", "verifier-rule", "rawMomentumVerifier.acknowledgementRecency")
		// Every user block confirmed by a momentum whose parent enforces
		// Dynamic Plasma must acknowledge a momentum at which every spork
		// enforced at that parent is already enforced — the momentum-side
		// arm of the acknowledgement-recency rule (#280). The requirement
		// is built from the parent (verifier/momentum.go passes
		// rmv.momentumStore, the store at m.Height-1), so a spork enforced
		// exactly at m.Height is not yet required of m's blocks.
		frontier, err := net.Frontier()
		if err != nil {
			t.Fatal(err)
		}
		checked := 0
		for depth := uint64(0); depth < 200 && frontier.Height-depth > dpEnf; depth++ {
			m := read(t, frontier.Height-depth)
			for _, entry := range m.Content {
				var blk *api.AccountBlock
				get(t, &blk, "ledger.getAccountBlockByHash", entry.Hash)
				if blk == nil || types.IsEmbeddedAddress(blk.Address) {
					continue
				}
				requirement := uint64(0)
				for _, h := range sporkHeights {
					if h <= m.Height-1 && h > requirement {
						requirement = h
					}
				}
				if blk.MomentumAcknowledged.Height < requirement {
					t.Errorf("block %s in momentum %d acknowledges momentum %d below the highest spork enforced at its parent %d",
						blk.Hash, m.Height, blk.MomentumAcknowledged.Height, requirement)
				}
				checked++
			}
			if checked > 0 {
				break // one momentum with user content is enough
			}
		}
		if checked == 0 {
			// A quiet devnet can have no user block in the window; make
			// one and check it the same way, so the case can neither pass
			// nor fail vacuously.
			funder := net.Key(IdxFunder)
			res := callOK(t, funder, types.PlasmaContract, definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, funder.Address), qsr, coins(10))
			m := read(t, res.Send.ConfirmationDetail.MomentumHeight)
			requirement := uint64(0)
			for _, h := range sporkHeights {
				if h <= m.Height-1 && h > requirement {
					requirement = h
				}
			}
			if res.Send.MomentumAcknowledged.Height < requirement {
				t.Errorf("fresh block %s in momentum %d acknowledges momentum %d below the highest spork enforced at its parent %d",
					res.Send.Hash, m.Height, res.Send.MomentumAcknowledged.Height, requirement)
			}
		}
	})

	t.Run("B-17_dynamic_plasma_boundary_from_history", func(t *testing.T) {
		if !dpKnown || dpEnf < 2 {
			// No Dynamic Plasma boundary on this chain: the pre-spork
			// invariant must hold at every momentum — version 1, zero
			// resource prices. Assert that instead of skipping, so the case
			// says something on every variant.
			frontier, err := net.Frontier()
			if err != nil {
				t.Fatal(err)
			}
			stretch := uint64(30)
			if frontier.Height < stretch+1 {
				stretch = frontier.Height - 1
			}
			if stretch < 1 {
				t.Fatalf("chain too short to walk: height %d", frontier.Height)
			}
			for h := frontier.Height - stretch + 1; h <= frontier.Height; h++ {
				m := read(t, h)
				if m.Version != 1 || m.NextFusionPrice != 0 || m.NextWorkPrice != 0 {
					t.Errorf("momentum %d: version %d prices %d/%d, want v1 0/0 with no dynamic plasma on this chain", h, m.Version, m.NextFusionPrice, m.NextWorkPrice)
				}
			}
			return
		}
		cover(t, "E7", "verifier-rule", "rawMomentumVerifier.version", "rawMomentumVerifier.nextFusionPrice", "rawMomentumVerifier.nextWorkPrice")
		// The version switch lands one momentum late: a producer builds
		// momentum N on its parent N-1's spork state, so at the enforcement
		// height the momentum is still Version 1 with zero prices.
		for _, h := range []uint64{dpEnf - 1, dpEnf, dpEnf + 1} {
			m := read(t, h)
			wantVersion, wantPriced := uint64(1), false
			if h > dpEnf {
				wantVersion, wantPriced = 2, true
			}
			if m.Version != wantVersion {
				t.Errorf("momentum %d: version %d, want %d", h, m.Version, wantVersion)
			}
			if wantPriced {
				if m.NextFusionPrice < dp.MinResourcePrice || m.NextWorkPrice < dp.MinResourcePrice {
					t.Errorf("momentum %d: prices %d/%d, want both >= %d", h, m.NextFusionPrice, m.NextWorkPrice, dp.MinResourcePrice)
				}
			} else if m.NextFusionPrice != 0 || m.NextWorkPrice != 0 {
				t.Errorf("momentum %d: prices %d/%d, want both zero before the switch", h, m.NextFusionPrice, m.NextWorkPrice)
			}
		}
	})
}

func contains(err error, sub string) bool {
	return err != nil && strings.Contains(err.Error(), sub)
}
