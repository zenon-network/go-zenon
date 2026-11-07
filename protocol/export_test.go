package protocol

// ReportImportFailure calls the import-failure hook the downloader and the
// fetcher call when InsertChain fails for a batch served by id.
func (pm *ProtocolManager) ReportImportFailure(id string, err error) { pm.rejectPeer(id, err) }

// Quarantined reports whether id is quarantined.
func (pm *ProtocolManager) Quarantined(id string) bool { return pm.quarantine.active(id) }
