package terminal

import (
	"context"
	"sync"
)

// SessionError is a state-machine refusal the client itself makes, before or
// instead of a control-plane answer. The code is the machine's own vocabulary;
// a control-plane failure reaches the caller as an APIError.
type SessionError struct {
	Code    string
	Message string
}

// Error renders the machine's stable code and its message.
func (e *SessionError) Error() string { return e.Code + ": " + e.Message }

// State-machine refusal codes.
const (
	// CodeNoSession is the answer before a session was created.
	CodeNoSession = "terminal_no_session"
	// CodeNotConnected is the answer to input while the session is not
	// connected (KOSD-C-90).
	CodeNotConnected = "terminal_not_connected"
	// CodeEnded is the answer to any action after the session left the live
	// set.
	CodeEnded = "terminal_session_ended"
	// CodeNoToken is the answer to an attach or input call without the one
	// token delivery that creates a session carried.
	CodeNoToken = "terminal_no_token"
)

// Session is one terminal session and its token and state machine
// (KOSD-C-90, KOSD-C-91). It holds the plaintext token in memory only: it
// never enters a URL, a query parameter or a log. It does not retry, reconnect
// or replay input, and a session that has left the live set refuses every
// further action (CONSOLE-C-11, CONSOLE-C-14).
type Session struct {
	mu           sync.Mutex
	client       *Client
	view         SessionView
	token        string
	sequences    map[uint64]InputReceipt
	nextSequence uint64
	ended        bool
}

// Open requests a session and holds the one token the control plane delivered
// on the first creation (KOSD-C-90, KOSD-C-91).
func (c *Client) Open(ctx context.Context, target Target) (*Session, error) {
	response, err := c.Create(ctx, target)
	if err != nil {
		return nil, err
	}
	return &Session{
		client:    c,
		view:      response.SessionView,
		token:     response.Token,
		sequences: map[uint64]InputReceipt{},
	}, nil
}

// View returns the session's current view as the control plane reported it.
func (s *Session) View() SessionView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view
}

// State returns the session's current state.
func (s *Session) State() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view.State
}

// CanInput reports whether the session accepts a new input batch: only a
// connected session does (KOSD-C-90).
func (s *Session) CanInput() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.ended && isLive(s.view.State) && s.view.State == StateConnected
}

// HasToken reports whether this session holds the token it was created with.
// A repeated creation returns the same session without a second token, so the
// caller it created for must still hold the original one.
func (s *Session) HasToken() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token != ""
}

// Attach connects the session with the token it holds. It is a single attempt:
// a failed attach does not retry or fall back to another credential
// (KOSD-C-91, CONSOLE-C-11).
func (s *Session) Attach(ctx context.Context) error {
	s.mu.Lock()
	if s.ended || !isLive(s.view.State) {
		s.mu.Unlock()
		return &SessionError{Code: CodeEnded, Message: "会话已结束，不能接入"}
	}
	token := s.token
	sessionID := s.view.SessionID
	s.mu.Unlock()
	if token == "" {
		return &SessionError{Code: CodeNoToken, Message: "该会话没有终端令牌，不能接入"}
	}
	view, err := s.client.Attach(ctx, sessionID, token)
	if err != nil {
		s.recordFailure(err)
		return err
	}
	s.mu.Lock()
	s.view = view
	s.mu.Unlock()
	return nil
}

// AttachWith connects the session with a token the caller supplies. It exists
// for the case where a repeated creation answers an existing session that this
// client did not create and therefore never received its token from.
func (s *Session) AttachWith(ctx context.Context, token string) error {
	s.mu.Lock()
	if s.ended || !isLive(s.view.State) {
		s.mu.Unlock()
		return &SessionError{Code: CodeEnded, Message: "会话已结束，不能接入"}
	}
	sessionID := s.view.SessionID
	s.mu.Unlock()
	if token == "" {
		return &SessionError{Code: CodeNoToken, Message: "终端令牌为空，不能接入"}
	}
	view, err := s.client.Attach(ctx, sessionID, token)
	if err != nil {
		s.recordFailure(err)
		return err
	}
	s.mu.Lock()
	s.view = view
	s.token = token
	s.mu.Unlock()
	return nil
}

// Send delivers one batch with the next per-session sequence number
// (KOSD-C-92). A repeated sequence is answered from the receipt already held
// and is never delivered again.
func (s *Session) Send(ctx context.Context, data []byte) (InputReceipt, error) {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return InputReceipt{}, &SessionError{Code: CodeEnded, Message: "会话已结束，不能输入"}
	}
	if s.view.SessionID == "" {
		s.mu.Unlock()
		return InputReceipt{}, &SessionError{Code: CodeNoSession, Message: "没有会话，不能输入"}
	}
	if !isLive(s.view.State) || s.view.State != StateConnected {
		s.mu.Unlock()
		return InputReceipt{}, &SessionError{Code: CodeNotConnected, Message: "只有已连接的会话接受输入"}
	}
	sequence := s.nextSequence
	if sequence == 0 {
		sequence = 1
	}
	sessionID, token := s.view.SessionID, s.token
	held, duplicate := s.sequences[sequence]
	s.mu.Unlock()
	if duplicate {
		held.Duplicate = true
		return held, nil
	}
	receipt, err := s.client.Input(ctx, sessionID, token, sequence, data)
	if err != nil {
		s.recordFailure(err)
		return InputReceipt{}, err
	}
	s.mu.Lock()
	s.sequences[sequence] = receipt
	if sequence >= s.nextSequence {
		s.nextSequence = sequence + 1
	}
	s.mu.Unlock()
	return receipt, nil
}

// SendSequence delivers one batch with the caller's sequence number. A caller
// that retries a delivery after a failure or a lost answer repeats the same
// number; the control plane then answers the original receipt and does not
// deliver the batch twice (KOSD-C-92).
func (s *Session) SendSequence(ctx context.Context, sequence uint64, data []byte) (InputReceipt, error) {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return InputReceipt{}, &SessionError{Code: CodeEnded, Message: "会话已结束，不能输入"}
	}
	if s.view.SessionID == "" {
		s.mu.Unlock()
		return InputReceipt{}, &SessionError{Code: CodeNoSession, Message: "没有会话，不能输入"}
	}
	if !isLive(s.view.State) || s.view.State != StateConnected {
		s.mu.Unlock()
		return InputReceipt{}, &SessionError{Code: CodeNotConnected, Message: "只有已连接的会话接受输入"}
	}
	held, duplicate := s.sequences[sequence]
	sessionID, token := s.view.SessionID, s.token
	s.mu.Unlock()
	if duplicate {
		held.Duplicate = true
		return held, nil
	}
	receipt, err := s.client.Input(ctx, sessionID, token, sequence, data)
	if err != nil {
		s.recordFailure(err)
		return InputReceipt{}, err
	}
	s.mu.Lock()
	s.sequences[sequence] = receipt
	if sequence >= s.nextSequence {
		s.nextSequence = sequence + 1
	}
	s.mu.Unlock()
	return receipt, nil
}

// Renew issues a new token for the session (KOSD-C-91). The new token makes the
// old one invalid and the idle bound is not reset (KOSD-C-94).
func (s *Session) Renew(ctx context.Context) error {
	s.mu.Lock()
	if s.ended || !isLive(s.view.State) {
		s.mu.Unlock()
		return &SessionError{Code: CodeEnded, Message: "会话已结束，不能续期"}
	}
	sessionID := s.view.SessionID
	s.mu.Unlock()
	response, err := s.client.Renew(ctx, sessionID)
	if err != nil {
		s.recordFailure(err)
		return err
	}
	s.mu.Lock()
	s.view = response.SessionView
	if response.Token != "" {
		s.token = response.Token
	}
	s.mu.Unlock()
	return nil
}

// Status refreshes the session's view. A view that left the live set ends the
// session here as well, so a later action is refused locally.
func (s *Session) Status(ctx context.Context) (SessionView, error) {
	s.mu.Lock()
	sessionID := s.view.SessionID
	s.mu.Unlock()
	if sessionID == "" {
		return s.View(), &SessionError{Code: CodeNoSession, Message: "没有会话，不能读取状态"}
	}
	view, err := s.client.Status(ctx, sessionID)
	if err != nil {
		s.recordFailure(err)
		return s.View(), err
	}
	s.mu.Lock()
	s.view = view
	if !isLive(view.State) {
		s.ended = true
	}
	s.mu.Unlock()
	return view, nil
}

// End ends the session. A repeated end has no further side effect
// (KOSD-C-90, CONSOLE-C-11).
func (s *Session) End(ctx context.Context) error {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return nil
	}
	sessionID := s.view.SessionID
	s.mu.Unlock()
	if sessionID == "" {
		s.mu.Lock()
		s.ended = true
		s.mu.Unlock()
		return nil
	}
	view, err := s.client.End(ctx, sessionID)
	if err != nil {
		s.recordFailure(err)
		s.mu.Lock()
		s.ended = true
		s.mu.Unlock()
		return err
	}
	s.mu.Lock()
	s.view = view
	s.ended = true
	s.mu.Unlock()
	return nil
}

// recordFailure moves the session out of the live set when the control plane
// reported a terminal failure, so no later action is offered against a session
// that can no longer accept input (CONSOLE-C-12).
func (s *Session) recordFailure(err error) {
	apiError, ok := err.(*APIError)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !isLive(s.view.State) {
		s.ended = true
		return
	}
	state := StateFailed
	if failureState(apiError.Code) != "" {
		state = failureState(apiError.Code)
	}
	s.view.State = state
	s.view.Reason = apiError.Code
	s.ended = state != StateConnected && state != StateConnecting
}

// failureState maps a terminal failure code to the state the session shows. A
// target or service failure is shown as failed with its own reason; a limit or
// idle end keeps the closed state (CONSOLE-C-12).
func failureState(code string) string {
	switch code {
	case "terminal_session_limit", "terminal_idle_timeout", "terminal_unauthenticated",
		"terminal_forbidden", "terminal_target_unavailable", "terminal_audit_unavailable":
		return StateClosed
	case "terminal_node_unreachable", "terminal_target_ended", "terminal_executor_unavailable",
		"terminal_credential_boundary_unavailable":
		return StateFailed
	default:
		return ""
	}
}

// isLive reports whether a state occupies a session and may still be acted on.
func isLive(state string) bool {
	return state == StateConnecting || state == StateConnected
}
