package protocol

import (
	"testing"
	"time"
)

// handoffStage is where a delivered batch is on its way from the
// downloader's queue to the chain, as the downloader's Stats and
// Processing report it (TestProcessingCoversTheImportHandoff in the
// downloader package establishes these readings against the real worker).
type handoffStage int

const (
	// stageIdle: nothing delivered, no worker.
	stageIdle handoffStage = iota
	// stageDelivered: the batch sits in the queue; Stats counts it as
	// cached, the worker has not started.
	stageDelivered
	// stageTaken: the worker has raised its flag and taken the batch but
	// not yet published it as importQueue; Stats sees nothing, only
	// Processing reports it.
	stageTaken
	// stageImporting: the batch is published as importQueue and being
	// inserted; Stats counts it as importing, Processing is still set.
	stageImporting
)

// handoffDownloader models the downloader at one handoff stage. With
// workerStartsAfterFirstRead set, the first of Stats or Processing to be
// called sees the stage as given and the worker then starts, so the
// second call sees stageTaken: the interleaving that the read order in
// readyToProduce exists for.
type handoffDownloader struct {
	stage                      handoffStage
	session                    bool
	workerStartsAfterFirstRead bool
	reads                      []string
	// beforeFirstRead, when set, runs once before the first of the three
	// reads is answered: something that happened in the downloader after
	// the predicate was entered and before it looked.
	beforeFirstRead func()
}

func (d *handoffDownloader) entered() {
	if d.beforeFirstRead != nil {
		hook := d.beforeFirstRead
		d.beforeFirstRead = nil
		hook()
	}
}

func (d *handoffDownloader) Synchronising() bool {
	d.entered()
	return d.session
}

func (d *handoffDownloader) Stats() (pending int, cached int, importing int, estimate time.Duration) {
	d.entered()
	defer d.afterRead("Stats")
	switch d.stage {
	case stageDelivered:
		return 0, 1, 0, 0
	case stageImporting:
		return 0, 0, 1, 0
	}
	return 0, 0, 0, 0
}

func (d *handoffDownloader) Processing() bool {
	d.entered()
	defer d.afterRead("Processing")
	return d.stage == stageTaken || d.stage == stageImporting
}

func (d *handoffDownloader) afterRead(name string) {
	d.reads = append(d.reads, name)
	if d.workerStartsAfterFirstRead {
		d.workerStartsAfterFirstRead = false
		d.stage = stageTaken
	}
}

// readinessPaths are the states in which, with no import work pending, the
// predicate says ready: caught up with the best advertised height, inside
// the stall window or past it, and behind that height but past the window.
// Inside the window a running session and blocks waiting in the queue hold
// production; past it they do not, and only blocks the worker owns do.
var readinessPaths = []struct {
	name   string
	height uint64
	td     uint64
	elapse time.Duration
}{
	{"caught up", 12, 12, 0},
	{"caught up, stall window expired", 12, 12, producerStallWindow},
	{"stall window expired", 10, 12, producerStallWindow},
}

// Every stage of the handoff at which the worker owns blocks holds
// production on every path that would otherwise be ready, and the idle
// downloader releases it on all of them. A running session, and a batch
// that is delivered but not taken, hold only inside the stall window: past
// it they are no more than the advertised height that led to them. The
// predicate under test is the body of ReadyToProduce, reading the model in
// place of the downloader.
func TestReadyToProduce_BlocksAtEveryHandoffStage(t *testing.T) {
	for _, path := range readinessPaths {
		t.Run(path.name, func(t *testing.T) {
			clock := useReadinessClock(t)
			pm, _ := newReadinessManager(t, 1, path.height)
			addPeer(t, pm, 1, path.td)
			clock.t = clock.t.Add(path.elapse)
			insideWindow := path.elapse < producerStallWindow

			if !pm.readyToProduce(&handoffDownloader{stage: stageIdle}) {
				t.Fatal("not ready with nothing delivered and no worker")
			}
			if ready := pm.readyToProduce(&handoffDownloader{stage: stageIdle, session: true}); ready == insideWindow {
				t.Fatalf("ready = %v while a session is running, inside the window = %v", ready, insideWindow)
			}
			if ready := pm.readyToProduce(&handoffDownloader{stage: stageDelivered}); ready == insideWindow {
				t.Fatalf("ready = %v with a batch delivered and not taken, inside the window = %v", ready, insideWindow)
			}
			for _, stage := range []handoffStage{stageTaken, stageImporting} {
				for _, session := range []bool{false, true} {
					if pm.readyToProduce(&handoffDownloader{stage: stage, session: session}) {
						t.Fatalf("ready at handoff stage %d (session %v)", stage, session)
					}
				}
			}
			// The real downloader behind the exported predicate is idle
			// here, so the same manager is ready through it as well.
			if !pm.ReadyToProduce() {
				t.Fatal("ReadyToProduce not ready with the real, idle downloader")
			}
		})
	}
}

// A worker that starts between the predicate's two downloader reads takes
// the batch out of the queue after one read and raises its flag before the
// other. Reading Stats first sees the batch cached and then, where the
// cached count no longer holds, the worker's flag; reading Processing
// first would see no worker and then an empty queue, and let production
// through while the worker owns blocks. This fails if the two reads are
// swapped.
func TestReadyToProduce_SeesAWorkerStartingBetweenTheReads(t *testing.T) {
	for _, path := range readinessPaths {
		t.Run(path.name, func(t *testing.T) {
			clock := useReadinessClock(t)
			pm, _ := newReadinessManager(t, 1, path.height)
			addPeer(t, pm, 1, path.td)
			clock.t = clock.t.Add(path.elapse)

			work := &handoffDownloader{stage: stageDelivered, workerStartsAfterFirstRead: true}
			if pm.readyToProduce(work) {
				t.Fatalf("ready although the worker took the batch between the reads (read order %v)", work.reads)
			}
			if len(work.reads) == 0 || work.reads[0] != "Stats" {
				t.Fatalf("the queue must be read before the worker flag, got %v", work.reads)
			}
		})
	}
}

// An import that finishes while the predicate runs, after it was entered
// with the window expired and before it reads the downloader, leaves the
// worker idle and the queue empty by the time they are read. The window is
// read after them, so the import is seen and the node, still behind, is
// held. This fails if the window is sampled before the downloader reads.
func TestReadyToProduce_SeesAnImportThatFinishedDuringTheReads(t *testing.T) {
	clock := useReadinessClock(t)
	pm, bridge := newReadinessManager(t, 1, 10)
	addPeer(t, pm, 1, 12)
	clock.t = clock.t.Add(producerStallWindow)
	if !pm.readyToProduce(&handoffDownloader{session: true}) {
		t.Fatal("not ready past the window with a session that imported nothing")
	}

	bridge.moved = true
	work := &handoffDownloader{session: true, beforeFirstRead: func() {
		if _, err := pm.insertFromDownload(nil); err != nil {
			t.Fatal(err)
		}
	}}
	if pm.readyToProduce(work) {
		t.Fatal("ready although a download import landed before the downloader was read")
	}
}
