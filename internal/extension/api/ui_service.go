package api

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// UIService holds in-memory UI state contributed by hosted extensions.
// Every mutation is keyed by extension owner so RemoveAllByOwner is safe.
type UIService struct {
	mu sync.RWMutex

	status      map[string]statusEntry
	widgets     map[string]map[string]ExtensionWidget
	dialogQueue []*dialogEntry
	dialogByID  map[string]*dialogEntry
}

type statusEntry struct {
	Text, Style string
}

type dialogEntry struct {
	ID    string
	Owner string
	Spec  DialogSpec
}

func NewUIService() *UIService {
	return &UIService{
		status:     map[string]statusEntry{},
		widgets:    map[string]map[string]ExtensionWidget{},
		dialogByID: map[string]*dialogEntry{},
	}
}

func (s *UIService) SetStatus(extID, text, style string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status[extID] = statusEntry{Text: text, Style: style}
	return nil
}

func (s *UIService) ClearStatus(extID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.status, extID)
	return nil
}

func (s *UIService) Status(extID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status[extID].Text
}

func (s *UIService) SetWidget(extID string, w ExtensionWidget) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.widgets[extID]
	if !ok {
		m = map[string]ExtensionWidget{}
		s.widgets[extID] = m
	}
	m[w.ID] = w
	return nil
}

func (s *UIService) ClearWidget(extID, widgetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.widgets[extID]; m != nil {
		delete(m, widgetID)
	}
	return nil
}

func (s *UIService) Widgets(extID string) []ExtensionWidget {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.widgets[extID]
	out := make([]ExtensionWidget, 0, len(m))
	for _, w := range m {
		out = append(out, w)
	}
	return out
}

func (s *UIService) EnqueueDialog(extID string, spec DialogSpec) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := newID()
	entry := &dialogEntry{ID: id, Owner: extID, Spec: spec}
	s.dialogQueue = append(s.dialogQueue, entry)
	s.dialogByID[id] = entry
	return id, nil
}

func (s *UIService) ActiveDialog() *dialogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.dialogQueue) == 0 {
		return nil
	}
	return s.dialogQueue[0]
}

func (s *UIService) ResolveDialog(id string, values map[string]any, cancelled bool, buttonID string) (DialogResolution, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.dialogByID[id]
	if !ok {
		return DialogResolution{}, false
	}
	delete(s.dialogByID, id)
	for i, e := range s.dialogQueue {
		if e.ID == id {
			s.dialogQueue = append(s.dialogQueue[:i], s.dialogQueue[i+1:]...)
			break
		}
	}
	return DialogResolution{
		DialogID: id, Values: values, Cancelled: cancelled, ButtonID: buttonID,
	}, true
}

func (s *UIService) DialogOwner(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.dialogByID[id]
	if !ok {
		return "", false
	}
	return e.Owner, true
}

// RemoveAllByOwner clears all UI state for an extension. Pending dialogs
// owned by the extension are returned as cancelled resolutions so the caller
// can dispatch ui.dialog.resolved with cancelled=true.
func (s *UIService) RemoveAllByOwner(extID string) []DialogResolution {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.status, extID)
	delete(s.widgets, extID)
	var cancelled []DialogResolution
	remaining := s.dialogQueue[:0]
	for _, e := range s.dialogQueue {
		if e.Owner == extID {
			delete(s.dialogByID, e.ID)
			cancelled = append(cancelled, DialogResolution{DialogID: e.ID, Cancelled: true})
			continue
		}
		remaining = append(remaining, e)
	}
	s.dialogQueue = remaining
	return cancelled
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
