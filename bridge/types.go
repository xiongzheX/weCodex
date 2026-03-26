package bridge

type InputKind string

const (
	InputPrompt InputKind = "prompt"
	InputHelp   InputKind = "help"
	InputStatus InputKind = "status"
	InputNew    InputKind = "new"
	InputList   InputKind = "list"
	InputUse    InputKind = "use"
)

type ParsedInput struct {
	Kind     InputKind
	Text     string
	UseIndex *int
}

type RuntimeStatus struct {
	BridgeMode       string
	BackendState     string
	HasActiveSession bool
	PermissionMode   string
	LastErrorSummary string
}

