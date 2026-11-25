package app

import (
	"net/http"

	"github.com/tsic404/kosd/pkg/extension"
	"github.com/tsic404/kosd/pkg/write"
)

// The display items of the terminal page, one per source type the design
// fixes (DASH-C-52): the target type, the node and Pod candidates, the session
// state and the session audit.
const (
	ItemTargetTypes  = "target-types"
	ItemNodeTargets  = "node-targets"
	ItemPodTargets   = "pod-targets"
	ItemSessionState = "session-state"
	ItemSessionAudit = "session-audit"
)

// Action identities of the terminal page (DASH-C-22). The browser submits
// both through the control plane; neither proxies a native resource write.
const (
	ActionTerminalCreate = "terminal-create"
	ActionTerminalEnd    = "terminal-end"
)

// Reasons a display item states instead of carrying a modification entry
// (KOSD-C-53). They are the page's own account of what each read means.
const (
	// TargetTypesReason explains how the page decides which target types the
	// account may use (CONSOLE-C-10).
	TargetTypesReason = "目标类型与执行身份按账号对目标的既有权限给出：节点仅全局 Owner，Pod 按所属应用的写权限"
	// NodeTargetsReason states that the node list carries only the targets the
	// account may connect to, and that a non-Owner sees no node type.
	NodeTargetsReason = "节点目标只列获准建立会话的节点，执行身份为 root@node；非 Owner 不显示节点类型"
	// PodTargetsReason states how Pod candidates are projected and filtered,
	// and that a multi-container Pod is never preselected (CONSOLE-C-8).
	PodTargetsReason = "Pod 候选来自所属应用的投影并按写权限过滤，明确命名空间、Pod 与容器；多容器不默选，单容器可预选但须显示"
	// EmptyTargetsReason is the page's statement when no permitted target
	// exists: no empty terminal prompt stands in for a session (CONSOLE-C-17).
	EmptyTargetsReason = "无可连接目标"
	// SessionStateReason states that session state, occupancy and the idle
	// bound come from the control-plane session surface, not from a projection.
	SessionStateReason = "会话状态、令牌到期、空闲时长与账号和目标的上限占用经控制面会话面读取，输入输出不经页面投影"
	// SessionAuditReason states that the audit read returns metadata only.
	SessionAuditReason = "会话事件经控制面审计读取，只展示获准读取的元数据，不记录原始输入、输出或令牌"
)

// reference is the item reference this product owns; the shell dispatches an
// item to the reader of the product in front of it (KOSD-C-18).
func reference(item string) string { return Identifier + "/" + item }

// TerminalPage declares the native terminal surface (CONSOLE-C-8, DASH-C-22).
//
// The page is open to signed-in Members and Owners; a Guest has no entry
// (CONSOLE-C-10), so the required role is Member. It declares no window
// application: the shell renders the page itself and the browser holds no
// iframe (DASH-C-22).
func TerminalPage() extension.Page {
	return extension.Page{
		ID:           PageTerminal,
		Title:        "终端",
		Location:     "desktop",
		RequiredRole: string(write.RoleMember),
		Items: []extension.PageItem{
			{ID: ItemTargetTypes, Label: "目标类型与执行身份", Source: extension.PageSourceComputed, Reference: reference(ItemTargetTypes), Reason: TargetTypesReason},
			{ID: ItemNodeTargets, Label: "节点目标", Source: extension.PageSourceObject, Reference: reference(ItemNodeTargets), Reason: NodeTargetsReason},
			{ID: ItemPodTargets, Label: "Pod 容器目标", Source: extension.PageSourceObject, Reference: reference(ItemPodTargets), Reason: PodTargetsReason},
			{ID: ItemSessionState, Label: "会话状态与占用", Source: extension.PageSourceRead, Reference: reference(ItemSessionState), Reason: SessionStateReason},
			{ID: ItemSessionAudit, Label: "会话审计", Source: extension.PageSourceDatabase, Reference: reference(ItemSessionAudit), Reason: SessionAuditReason},
		},
		Actions: []extension.Action{
			{ID: ActionTerminalCreate, Method: http.MethodPost, Path: PathTerminalSessions},
			{ID: ActionTerminalEnd, Method: http.MethodDelete, Path: PathTerminalSession},
		},
	}
}
