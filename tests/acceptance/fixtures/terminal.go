// Package fixtures boots the pieces the terminal acceptance cases need: a
// scripted control-plane terminal surface over an in-memory world. Nothing here
// reaches a real cluster, database or executor, and no case can observe a
// value the design forbids.
package fixtures

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tsic404/kos-console/terminal"
)

// Terminal is one scripted control plane's terminal surface. It answers the
// same routes and the same envelopes the control plane serves (KOSD-C-90 to
// KOSD-C-96), so a case drives the real client against a surface that behaves
// the way the contract describes.
type Terminal struct {
	mu sync.Mutex

	consoleToken string
	extension    string
	active       bool

	accountLimit int
	targetLimit  int
	idleMinutes  int

	nodeTargets []terminal.NodeTarget

	sessions map[string]*fakeSession
	byToken  map[string]string
	accounts map[string]int
	targets  map[string]int

	// failureOn names the route a configured failure applies to: create,
	// attach, input, status or audit.
	failureOn string
	failure   *failureSpec

	auditDown  bool
	output     string
	endCalls   int
	inputCalls int
}

// failureSpec is one scripted control-plane failure.
type failureSpec struct {
	Status  int    `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Stage   string `json:"stage"`
}

// Options shapes one scripted terminal surface.
type Options struct {
	// ConsoleToken is the console session the gated routes accept.
	ConsoleToken string
	// Extension is the component identifier the calls act for.
	Extension string
	// Active reports whether the component is installed (KOSD-F-16).
	Active bool
	// AccountLimit and TargetLimit are the session bounds (KOSD-C-90).
	AccountLimit int
	TargetLimit  int
	// IdleMinutes is the idle bound every session reports (KOSD-C-94).
	IdleMinutes int
	// Nodes is the node inventory the node read returns (KOSD-C-95).
	Nodes []terminal.NodeTarget
}

// NewTerminal builds a scripted terminal surface with the option defaults.
func NewTerminal(options Options) *Terminal {
	terminal := &Terminal{
		consoleToken: options.ConsoleToken,
		extension:    options.Extension,
		active:       options.Active,
		accountLimit: options.AccountLimit,
		targetLimit:  options.TargetLimit,
		idleMinutes:  options.IdleMinutes,
		nodeTargets:  options.Nodes,
		sessions:     map[string]*fakeSession{},
		byToken:      map[string]string{},
		accounts:     map[string]int{},
		targets:      map[string]int{},
	}
	if terminal.accountLimit == 0 {
		terminal.accountLimit = 4
	}
	if terminal.targetLimit == 0 {
		terminal.targetLimit = 4
	}
	if terminal.idleMinutes == 0 {
		terminal.idleMinutes = 10
	}
	return terminal
}

// Handler returns the HTTP handler of the scripted surface.
func (t *Terminal) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/terminal/sessions", t.create)
	mux.HandleFunc("GET /api/v1/terminal/sessions/{sessionId}", t.status)
	mux.HandleFunc("POST /api/v1/terminal/sessions/{sessionId}/renew", t.renew)
	mux.HandleFunc("POST /api/v1/terminal/sessions/{sessionId}/attach", t.attach)
	mux.HandleFunc("POST /api/v1/terminal/sessions/{sessionId}/input", t.input)
	mux.HandleFunc("DELETE /api/v1/terminal/sessions/{sessionId}", t.end)
	mux.HandleFunc("GET /api/v1/terminal/sessions/{sessionId}/audit", t.audit)
	mux.HandleFunc("GET /api/v1/terminal/nodes", t.nodes)
	return mux
}

// SetFailure scripts one failure onto one route.
func (t *Terminal) SetFailure(route string, status int, code, message, stage string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failureOn = route
	t.failure = &failureSpec{Status: status, Code: code, Message: message, Stage: stage}
}

// ClearFailure removes a scripted failure.
func (t *Terminal) ClearFailure() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failureOn = ""
	t.failure = nil
}

// SetAuditDown makes every audit read fail (KOSD-C-92).
func (t *Terminal) SetAuditDown(down bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.auditDown = down
}

// SetActive installs or withdraws the component. Withdrawal ends every live
// session and invalidates its token; reinstalling restores only the ability to
// establish a new one (CONSOLE-C-16).
func (t *Terminal) SetActive(active bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active = active
	if active {
		return
	}
	for _, session := range t.sessions {
		if session.live() {
			session.state = terminal.StateClosed
			session.reason = "terminal_session_ended"
			session.view.State = terminal.StateClosed
			session.view.Reason = session.reason
			delete(t.byToken, session.token)
		}
	}
	t.accounts = map[string]int{}
	t.targets = map[string]int{}
}

// SessionCount reports how many sessions the surface holds, live or ended.
func (t *Terminal) SessionCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sessions)
}

// SetOutput changes the output a delivered batch reports.
func (t *Terminal) SetOutput(output string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.output = output
}

// EndCalls reports how many end requests the surface received.
func (t *Terminal) EndCalls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.endCalls
}

// InputCalls reports how many input requests the surface delivered.
func (t *Terminal) InputCalls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.inputCalls
}

// fakeSession is one session the scripted surface holds.
type fakeSession struct {
	view     terminal.SessionView
	token    string
	state    string
	reason   string
	receipts map[uint64]terminal.InputReceipt
	next     uint64
}

// live reports whether the session still occupies a slot.
func (s *fakeSession) live() bool {
	return s.state == terminal.StateConnecting || s.state == terminal.StateConnected
}

func (t *Terminal) create(w http.ResponseWriter, r *http.Request) {
	if !t.checkAuth(w, r, t.consoleToken) {
		return
	}
	if scripted := t.scripted(w, "create"); scripted {
		return
	}
	var request struct {
		Extension  string `json:"extension"`
		TargetType string `json:"targetType"`
		TargetID   string `json:"targetId"`
		TargetUID  string `json:"targetUid"`
		Namespace  string `json:"namespace"`
		Container  string `json:"container"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_field", "请求体无法解析", "validate")
		return
	}
	target := terminal.Target{
		Type:      request.TargetType,
		ID:        request.TargetID,
		UID:       request.TargetUID,
		Namespace: request.Namespace,
		Container: request.Container,
	}
	if request.Extension == "" || target.Type == "" || target.ID == "" || target.UID == "" {
		writeError(w, http.StatusBadRequest, "invalid_field", "必须给出组件与目标标识", "validate")
		return
	}
	if target.Type == terminal.TargetNode && (target.Namespace != "" || target.Container != "") {
		writeError(w, http.StatusBadRequest, "invalid_field", "节点目标的命名空间与容器必须是空字符串", "validate")
		return
	}
	if target.Type == terminal.TargetPod && (target.Namespace == "" || target.Container == "") {
		writeError(w, http.StatusBadRequest, "invalid_field", "Pod 目标必须给出命名空间与容器", "validate")
		return
	}
	if !t.active {
		writeError(w, http.StatusServiceUnavailable, "capability_missing", "终端组件未激活，不能建立会话", "capability")
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.auditDown {
		writeError(w, http.StatusServiceUnavailable, "terminal_audit_unavailable", "终端审计不可用", "connect")
		return
	}
	account := t.consoleToken
	for _, session := range t.sessions {
		if session.live() && session.view.TargetUID == target.UID &&
			session.view.Container == target.Container && session.view.TargetType == target.Type {
			writeJSON(w, http.StatusOK, session.view)
			return
		}
	}
	if t.accounts[account] >= t.accountLimit || t.targets[target.UID] >= t.targetLimit {
		writeError(w, http.StatusTooManyRequests, "terminal_session_limit", "账号或目标的终端会话已达上限，需先释放名额", "connect")
		return
	}
	token := newToken()
	now := time.Now().UTC()
	session := &fakeSession{
		view: terminal.SessionView{
			SessionID:          newID(),
			TargetType:         target.Type,
			TargetID:           target.ID,
			TargetUID:          target.UID,
			Namespace:          target.Namespace,
			Container:          target.Container,
			ExecutionIdentity:  executionIdentity(target),
			State:              terminal.StateConnecting,
			ExpiresAt:          now.Add(5 * time.Minute),
			IdleTimeoutMinutes: t.idleMinutes,
			Limits:             terminal.Limits{Account: t.accountLimit, Target: t.targetLimit},
		},
		token:    token,
		state:    terminal.StateConnecting,
		receipts: map[uint64]terminal.InputReceipt{},
	}
	t.sessions[session.view.SessionID] = session
	t.byToken[token] = session.view.SessionID
	t.accounts[account]++
	t.targets[target.UID]++
	session.view.Usage = t.usage(account, target.UID)
	response := struct {
		terminal.SessionView
		Token string `json:"token,omitempty"`
	}{session.view, token}
	writeJSON(w, http.StatusCreated, response)
}

func (t *Terminal) status(w http.ResponseWriter, r *http.Request) {
	if !t.checkAuth(w, r, t.consoleToken) {
		return
	}
	if scripted := t.scripted(w, "status"); scripted {
		return
	}
	session, ok := t.session(r)
	if !ok {
		writeError(w, http.StatusForbidden, "terminal_forbidden", "没有可读的终端会话", "authorize")
		return
	}
	writeJSON(w, http.StatusOK, t.view(session))
}

func (t *Terminal) renew(w http.ResponseWriter, r *http.Request) {
	if !t.checkAuth(w, r, t.consoleToken) {
		return
	}
	if scripted := t.scripted(w, "renew"); scripted {
		return
	}
	session, ok := t.session(r)
	if !ok {
		writeError(w, http.StatusForbidden, "terminal_forbidden", "没有可续期的终端会话", "authorize")
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !session.live() {
		writeError(w, http.StatusConflict, "terminal_target_ended", "会话已结束，不能续期", "connect")
		return
	}
	token := newToken()
	previous := session.token
	session.token = token
	session.view.ExpiresAt = time.Now().UTC().Add(5 * time.Minute)
	delete(t.byToken, previous)
	t.byToken[token] = session.view.SessionID
	response := struct {
		terminal.SessionView
		Token string `json:"token,omitempty"`
	}{t.view(session), token}
	writeJSON(w, http.StatusOK, response)
}

func (t *Terminal) attach(w http.ResponseWriter, r *http.Request) {
	session, ok := t.sessionByToken(w, r)
	if !ok {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !session.live() {
		writeError(w, http.StatusUnauthorized, "terminal_unauthenticated", "会话已结束，令牌已失效", "authorize")
		return
	}
	session.state = terminal.StateConnected
	session.view.State = terminal.StateConnected
	session.view.ExpiresAt = time.Now().UTC().Add(5 * time.Minute)
	writeJSON(w, http.StatusOK, t.view(session))
}

func (t *Terminal) input(w http.ResponseWriter, r *http.Request) {
	session, ok := t.sessionByToken(w, r)
	if !ok {
		return
	}
	t.mu.Lock()
	t.inputCalls++
	down := t.auditDown
	t.mu.Unlock()
	if down {
		t.mu.Lock()
		session.state = terminal.StateClosed
		session.reason = "terminal_audit_unavailable"
		session.view.State = terminal.StateClosed
		session.view.Reason = session.reason
		delete(t.byToken, session.token)
		t.releaseLocked(session)
		t.mu.Unlock()
		writeError(w, http.StatusServiceUnavailable, "terminal_audit_unavailable", "终端审计不可用，会话已结束", "connect")
		return
	}
	if scripted := t.scripted(w, "input"); scripted {
		t.mu.Lock()
		if session.live() {
			session.state = terminal.StateClosed
			session.reason = t.failure.Code
			session.view.State = terminal.StateClosed
			session.view.Reason = session.reason
			delete(t.byToken, session.token)
			t.releaseLocked(session)
		}
		t.mu.Unlock()
		return
	}
	var request struct {
		Sequence uint64 `json:"sequence"`
		Data     string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Sequence == 0 || request.Data == "" {
		writeError(w, http.StatusBadRequest, "invalid_field", "输入批次需要从 1 开始的序号与非空内容", "validate")
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !session.live() {
		writeError(w, http.StatusConflict, "terminal_target_ended", "只有已连接的会话接受输入", "connect")
		return
	}
	if session.state != terminal.StateConnected {
		writeError(w, http.StatusConflict, "terminal_target_ended", "只有已连接的会话接受输入", "connect")
		return
	}
	if receipt, duplicate := session.receipts[request.Sequence]; duplicate {
		receipt.Duplicate = true
		writeJSON(w, http.StatusOK, receipt)
		return
	}
	receipt := terminal.InputReceipt{
		Sequence: request.Sequence,
		Result:   "accepted",
		Output:   t.output,
	}
	session.receipts[request.Sequence] = receipt
	writeJSON(w, http.StatusOK, receipt)
}

func (t *Terminal) end(w http.ResponseWriter, r *http.Request) {
	if !t.checkAuth(w, r, t.consoleToken) {
		return
	}
	session, ok := t.session(r)
	if !ok {
		writeError(w, http.StatusForbidden, "terminal_forbidden", "没有可结束的终端会话", "authorize")
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.endCalls++
	if session.live() {
		session.state = terminal.StateClosed
		session.reason = "terminal_session_ended"
		session.view.State = terminal.StateClosed
		session.view.Reason = session.reason
		delete(t.byToken, session.token)
		t.releaseLocked(session)
	}
	writeJSON(w, http.StatusOK, t.view(session))
}

// releaseLocked returns one session's account and target slots.
func (t *Terminal) releaseLocked(session *fakeSession) {
	t.accounts[t.consoleToken]--
	t.targets[session.view.TargetUID]--
	if t.accounts[t.consoleToken] <= 0 {
		delete(t.accounts, t.consoleToken)
	}
	if t.targets[session.view.TargetUID] <= 0 {
		delete(t.targets, session.view.TargetUID)
	}
}

func (t *Terminal) audit(w http.ResponseWriter, r *http.Request) {
	if !t.checkAuth(w, r, t.consoleToken) {
		return
	}
	t.mu.Lock()
	down := t.auditDown
	t.mu.Unlock()
	if down {
		writeError(w, http.StatusServiceUnavailable, "terminal_audit_unavailable", "终端审计不可用", "read")
		return
	}
	if scripted := t.scripted(w, "audit"); scripted {
		return
	}
	session, ok := t.session(r)
	if !ok {
		writeError(w, http.StatusForbidden, "terminal_forbidden", "没有可读的会话审计", "authorize")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": []map[string]any{
			{
				"eventId": 1, "actor": t.consoleToken, "extension": t.extension,
				"sessionId": session.view.SessionID, "targetType": session.view.TargetType,
				"targetId": session.view.TargetID, "targetUid": session.view.TargetUID,
				"namespace": session.view.Namespace, "container": session.view.Container,
				"executionIdentity": session.view.ExecutionIdentity, "event": "establish",
				"result": "accepted", "occurredAt": time.Now().UTC(),
			},
		},
	})
}

func (t *Terminal) nodes(w http.ResponseWriter, r *http.Request) {
	if !t.checkAuth(w, r, t.consoleToken) {
		return
	}
	if scripted := t.scripted(w, "nodes"); scripted {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		writeError(w, http.StatusServiceUnavailable, "capability_missing", "终端组件未激活，不能读取节点清单", "capability")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": t.nodeTargets})
}

// session resolves one session by the path identity.
func (t *Terminal) session(r *http.Request) (*fakeSession, bool) {
	id := r.PathValue("sessionId")
	t.mu.Lock()
	defer t.mu.Unlock()
	session, ok := t.sessions[id]
	return session, ok
}

// sessionByToken resolves one session by the terminal token, which is the only
// credential the attach and input routes accept.
func (t *Terminal) sessionByToken(w http.ResponseWriter, r *http.Request) (*fakeSession, bool) {
	token := bearer(r)
	t.mu.Lock()
	defer t.mu.Unlock()
	if token == "" {
		writeError(w, http.StatusUnauthorized, "terminal_unauthenticated", "缺少终端令牌", "authorize")
		return nil, false
	}
	id, ok := t.byToken[token]
	if !ok {
		writeError(w, http.StatusUnauthorized, "terminal_unauthenticated", "终端令牌无效或已失效", "authorize")
		return nil, false
	}
	session, ok := t.sessions[id]
	if !ok || session.view.SessionID != r.PathValue("sessionId") {
		writeError(w, http.StatusForbidden, "terminal_forbidden", "令牌与终端会话不符", "authorize")
		return nil, false
	}
	return session, true
}

// checkAuth refuses a gated request whose console session is not the one this
// surface serves.
func (t *Terminal) checkAuth(w http.ResponseWriter, r *http.Request, expected string) bool {
	if bearer(r) != expected || expected == "" {
		writeError(w, http.StatusUnauthorized, "terminal_unauthenticated", "终端会话缺少有效的控制台登录", "authorize")
		return false
	}
	return true
}

// scripted writes a configured failure for one route, if one is set.
func (t *Terminal) scripted(w http.ResponseWriter, route string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.failureOn != route || t.failure == nil {
		return false
	}
	writeError(w, t.failure.Status, t.failure.Code, t.failure.Message, t.failure.Stage)
	return true
}

// view renders one session with the occupancy a reader sees (KOSD-C-90).
func (t *Terminal) view(session *fakeSession) terminal.SessionView {
	view := session.view
	view.State = session.state
	view.Reason = session.reason
	view.Usage = t.usage(t.consoleToken, view.TargetUID)
	return view
}

// usage counts the sessions the account and the target hold (KOSD-C-94).
func (t *Terminal) usage(account, targetUID string) terminal.Usage {
	return terminal.Usage{Account: t.accounts[account], Target: t.targets[targetUID]}
}

// executionIdentity names the identity a session reaches its target as
// (KOSD-C-93).
func executionIdentity(target terminal.Target) string {
	if target.Type == terminal.TargetNode {
		return "root@node"
	}
	return "container:" + target.Namespace + "/" + target.ID + "/" + target.Container
}

// bearer reads the Authorization header.
func bearer(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

// newToken returns a random token.
func newToken() string {
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// newID returns a random session identifier.
func newID() string {
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	return "ts-" + base64.RawURLEncoding.EncodeToString(raw)
}

// writeJSON writes a JSON answer.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError writes the failure envelope (KOSD-C-12).
func writeError(w http.ResponseWriter, status int, code, message, stage string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":               code,
			"message":            message,
			"stage":              stage,
			"retryable":          false,
			"requiresUserAction": false,
			"sideEffects":        "none",
		},
	})
}
