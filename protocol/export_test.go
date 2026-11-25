package protocol

import "time"

// ReportImportFailure calls the import-failure hook the downloader and the
// fetcher call when InsertChain fails for a batch served by id.
func (pm *ProtocolManager) ReportImportFailure(id string, err error) { pm.rejectPeer(id, err) }

// Quarantined reports whether id is quarantined.
func (pm *ProtocolManager) Quarantined(id string) bool { return pm.quarantine.active(id) }

// ConfigureEmbeddedReceiveCacheForTest overrides the bridge's embedded-receive
// cache patch limit, retry cooldown, and clock for deterministic tests.
func ConfigureEmbeddedReceiveCacheForTest(bridge ChainBridge, maxPatchBytes int, cooldown time.Duration, now func() time.Time) {
	if cb, ok := bridge.(chainBridge); ok && cb.embeddedReceives != nil {
		cb.embeddedReceives.mu.Lock()
		defer cb.embeddedReceives.mu.Unlock()
		cb.embeddedReceives.maxPatchBytes = maxPatchBytes
		cb.embeddedReceives.retryCooldown = cooldown
		cb.embeddedReceives.now = now
	}
}
