package lsp

import (
	"github.com/lizongying/nolang/parser"
)

const (
	ProtocolVersion = 3

	MessageTypeError   = 1
	MessageTypeWarning = 2
	MessageTypeInfo    = 3
	MessageTypeLog     = 4

	TextDocumentSyncKindNone        = 0
	TextDocumentSyncKindFull        = 1
	TextDocumentSyncKindIncremental = 2

	CompletionItemKindText          = 1
	CompletionItemKindMethod        = 2
	CompletionItemKindFunction      = 3
	CompletionItemKindConstructor   = 4
	CompletionItemKindField         = 5
	CompletionItemKindVariable      = 6
	CompletionItemKindClass         = 7
	CompletionItemKindInterface     = 8
	CompletionItemKindModule        = 9
	CompletionItemKindProperty      = 10
	CompletionItemKindUnit          = 11
	CompletionItemKindValue         = 12
	CompletionItemKindEnum          = 13
	CompletionItemKindKeyword       = 14
	CompletionItemKindSnippet       = 15
	CompletionItemKindColor         = 16
	CompletionItemKindFile          = 17
	CompletionItemKindReference     = 18
	CompletionItemKindFolder        = 19
	CompletionItemKindEnumMember    = 20
	CompletionItemKindConstant      = 21
	CompletionItemKindStruct        = 22
	CompletionItemKindEvent         = 23
	CompletionItemKindOperator      = 24
	CompletionItemKindTypeParameter = 25

	InsertTextFormatPlainText = 1
	InsertTextFormatSnippet   = 2

	MarkupKindPlainText = "plaintext"
	MarkupKindMarkdown  = "markdown"

	DiagnosticSeverityError   = 1
	DiagnosticSeverityWarning = 2
	DiagnosticSeverityInfo    = 3
	DiagnosticSeverityHint    = 4
)

type Position struct {
	Line      uint32 `json:"line"`
	Character uint32 `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type VersionedTextDocumentIdentifier struct {
	TextDocumentIdentifier
	Version int `json:"version"`
}

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type InitializeParams struct {
	ProcessID             *int64                 `json:"processId,omitempty"`
	ClientInfo            *ClientInfo            `json:"clientInfo,omitempty"`
	Locale                string                 `json:"locale,omitempty"`
	RootURI               *string                `json:"rootUri,omitempty"`
	InitializationOptions interface{}            `json:"initializationOptions,omitempty"`
	Capabilities          ClientCapabilities     `json:"capabilities"`
	WorkspaceFolders      []WorkspaceFolder      `json:"workspaceFolders,omitempty"`
	Trace                 string                 `json:"trace,omitempty"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type ClientCapabilities struct {
	Workspace    WorkspaceClientCapabilities    `json:"workspace,omitempty"`
	TextDocument TextDocumentClientCapabilities `json:"textDocument,omitempty"`
	Window       WindowClientCapabilities       `json:"window,omitempty"`
	General      GeneralClientCapabilities      `json:"general,omitempty"`
	Experimental map[string]interface{}         `json:"experimental,omitempty"`
}

type WorkspaceClientCapabilities struct {
	ApplyEdit              bool                                `json:"applyEdit,omitempty"`
	WorkspaceEdit          *WorkspaceEditCapabilities          `json:"workspaceEdit,omitempty"`
	DidChangeConfiguration *DidChangeConfigurationCapabilities `json:"didChangeConfiguration,omitempty"`
	DidChangeWatchedFiles  *DidChangeWatchedFilesCapabilities  `json:"didChangeWatchedFiles,omitempty"`
	Symbol                 *SymbolCapabilities                 `json:"symbol,omitempty"`
	ExecuteCommand         *ExecuteCommandCapabilities         `json:"executeCommand,omitempty"`
	WorkspaceFolders       bool                                `json:"workspaceFolders,omitempty"`
	Configuration          bool                                `json:"configuration,omitempty"`
}

type WorkspaceEditCapabilities struct {
	DocumentChanges bool `json:"documentChanges,omitempty"`
}

type DidChangeConfigurationCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type DidChangeWatchedFilesCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type SymbolCapabilities struct {
	DynamicRegistration bool                    `json:"dynamicRegistration,omitempty"`
	SymbolKind          *SymbolKindCapabilities `json:"symbolKind,omitempty"`
}

type SymbolKindCapabilities struct {
	ValueSet []int `json:"valueSet,omitempty"`
}

type ExecuteCommandCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type TextDocumentClientCapabilities struct {
	Synchronization   *SynchronizationCapabilities   `json:"synchronization,omitempty"`
	Completion        *CompletionCapabilities        `json:"completion,omitempty"`
	Hover             *HoverCapabilities             `json:"hover,omitempty"`
	SignatureHelp     *SignatureHelpCapabilities     `json:"signatureHelp,omitempty"`
	Declaration       *DeclarationCapabilities       `json:"declaration,omitempty"`
	Definition        *DefinitionCapabilities        `json:"definition,omitempty"`
	TypeDefinition    *TypeDefinitionCapabilities    `json:"typeDefinition,omitempty"`
	Implementation    *ImplementationCapabilities    `json:"implementation,omitempty"`
	References        *ReferencesCapabilities        `json:"references,omitempty"`
	DocumentHighlight *DocumentHighlightCapabilities `json:"documentHighlight,omitempty"`
	DocumentSymbol    *DocumentSymbolCapabilities    `json:"documentSymbol,omitempty"`
	Formatting        *FormattingCapabilities        `json:"formatting,omitempty"`
	RangeFormatting   *RangeFormattingCapabilities   `json:"rangeFormatting,omitempty"`
	OnTypeFormatting  *OnTypeFormattingCapabilities  `json:"onTypeFormatting,omitempty"`
	CodeAction        *CodeActionCapabilities        `json:"codeAction,omitempty"`
	CodeLens          *CodeLensCapabilities          `json:"codeLens,omitempty"`
	Link              *LinkCapabilities              `json:"link,omitempty"`
}

type SynchronizationCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	WillSave            bool `json:"willSave,omitempty"`
	WillSaveWaitUntil   bool `json:"willSaveWaitUntil,omitempty"`
	DidSave             bool `json:"didSave,omitempty"`
}

type CompletionCapabilities struct {
	DynamicRegistration bool                            `json:"dynamicRegistration,omitempty"`
	CompletionItem      *CompletionItemCapabilities     `json:"completionItem,omitempty"`
	CompletionItemKind  *CompletionItemKindCapabilities `json:"completionItemKind,omitempty"`
	Context             bool                            `json:"contextSupport,omitempty"`
}

type CompletionItemCapabilities struct {
	SnippetSupport   bool                    `json:"snippetSupport,omitempty"`
	CommitCharacters bool                    `json:"commitCharactersSupport,omitempty"`
	Markdown         *MarkdownCapabilities   `json:"markdown,omitempty"`
	PreselectSupport bool                    `json:"preselectSupport,omitempty"`
	TagSupport       *TagSupportCapabilities `json:"tagSupport,omitempty"`
}

type MarkdownCapabilities struct {
	Parser  string `json:"parser"`
	Version string `json:"version,omitempty"`
}

type TagSupportCapabilities struct {
	ValueSet []int `json:"valueSet"`
}

type CompletionItemKindCapabilities struct {
	ValueSet []int `json:"valueSet,omitempty"`
}

type HoverCapabilities struct {
	DynamicRegistration bool     `json:"dynamicRegistration,omitempty"`
	ContentFormat       []string `json:"contentFormat,omitempty"`
}

type SignatureHelpCapabilities struct {
	DynamicRegistration  bool                              `json:"dynamicRegistration,omitempty"`
	SignatureInformation *SignatureInformationCapabilities `json:"signatureInformation,omitempty"`
	ContextSupport       bool                              `json:"contextSupport,omitempty"`
}

type SignatureInformationCapabilities struct {
	DocumentationFormat  []string                          `json:"documentationFormat,omitempty"`
	ParameterInformation *ParameterInformationCapabilities `json:"parameterInformation,omitempty"`
}

type ParameterInformationCapabilities struct {
	LabelOffsetSupport bool `json:"labelOffsetSupport,omitempty"`
}

type DeclarationCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	LinkSupport         bool `json:"linkSupport,omitempty"`
}

type DefinitionCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	LinkSupport         bool `json:"linkSupport,omitempty"`
}

type TypeDefinitionCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	LinkSupport         bool `json:"linkSupport,omitempty"`
}

type ImplementationCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	LinkSupport         bool `json:"linkSupport,omitempty"`
}

type ReferencesCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type DocumentHighlightCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type DocumentSymbolCapabilities struct {
	DynamicRegistration               bool                    `json:"dynamicRegistration,omitempty"`
	SymbolKind                        *SymbolKindCapabilities `json:"symbolKind,omitempty"`
	HierarchicalDocumentSymbolSupport bool                    `json:"hierarchicalDocumentSymbolSupport,omitempty"`
}

type FormattingCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type RangeFormattingCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type OnTypeFormattingCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type CodeActionCapabilities struct {
	DynamicRegistration      bool                                  `json:"dynamicRegistration,omitempty"`
	CodeActionLiteralSupport *CodeActionLiteralSupportCapabilities `json:"codeActionLiteralSupport,omitempty"`
}

type CodeActionLiteralSupportCapabilities struct {
	CodeActionKind ValueSetCapabilities `json:"codeActionKind"`
}

type ValueSetCapabilities struct {
	ValueSet []interface{} `json:"valueSet,omitempty"`
}

type CodeLensCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type LinkCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type WindowClientCapabilities struct {
	WorkDoneProgress bool `json:"workDoneProgress,omitempty"`
}

type GeneralClientCapabilities struct {
	StaleRequestSupport *StaleRequestCapabilities `json:"staleRequestSupport,omitempty"`
}

type StaleRequestCapabilities struct {
	Cancel bool `json:"cancel"`
}

type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

type ServerCapabilities struct {
	TextDocumentSync                 *TextDocumentSyncOptions         `json:"textDocumentSync,omitempty"`
	CompletionProvider               *CompletionOptions               `json:"completionProvider,omitempty"`
	HoverProvider                    bool                             `json:"hoverProvider,omitempty"`
	SignatureHelpProvider            *SignatureHelpOptions            `json:"signatureHelpProvider,omitempty"`
	DeclarationProvider              bool                             `json:"declarationProvider,omitempty"`
	DefinitionProvider               bool                             `json:"definitionProvider,omitempty"`
	TypeDefinitionProvider           bool                             `json:"typeDefinitionProvider,omitempty"`
	ImplementationProvider           bool                             `json:"implementationProvider,omitempty"`
	ReferencesProvider               bool                             `json:"referencesProvider,omitempty"`
	DocumentHighlightProvider        bool                             `json:"documentHighlightProvider,omitempty"`
	DocumentSymbolProvider           bool                             `json:"documentSymbolProvider,omitempty"`
	CodeActionProvider               bool                             `json:"codeActionProvider,omitempty"`
	CodeLensProvider                 *CodeLensOptions                 `json:"codeLensProvider,omitempty"`
	DocumentFormattingProvider       bool                             `json:"documentFormattingProvider,omitempty"`
	DocumentRangeFormattingProvider  bool                             `json:"documentRangeFormattingProvider,omitempty"`
	DocumentOnTypeFormattingProvider *DocumentOnTypeFormattingOptions `json:"documentOnTypeFormattingProvider,omitempty"`
	RenameProvider                   bool                             `json:"renameProvider,omitempty"`
	FoldingRangeProvider             bool                             `json:"foldingRangeProvider,omitempty"`
	ExecuteCommandProvider           *ExecuteCommandOptions           `json:"executeCommandProvider,omitempty"`
	WorkspaceSymbolProvider          bool                             `json:"workspaceSymbolProvider,omitempty"`
	WorkspaceFolders                 *WorkspaceFoldersOptions         `json:"workspaceFolders,omitempty"`
	ColorProvider                    bool                             `json:"colorProvider,omitempty"`
}

type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   *ServerInfo        `json:"serverInfo,omitempty"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type TextDocumentSyncOptions struct {
	OpenClose         bool         `json:"openClose,omitempty"`
	Save              *SaveOptions `json:"save,omitempty"`
	Change            int          `json:"change,omitempty"`
	WillSave          bool         `json:"willSave,omitempty"`
	WillSaveWaitUntil bool         `json:"willSaveWaitUntil,omitempty"`
}

type SaveOptions struct {
	IncludeText bool `json:"includeText,omitempty"`
}

type CompletionOptions struct {
	ResolveProvider   bool     `json:"resolveProvider,omitempty"`
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
	WorkDoneProgress  bool     `json:"workDoneProgress,omitempty"`
}

type SignatureHelpOptions struct {
	TriggerCharacters   []string `json:"triggerCharacters,omitempty"`
	RetriggerCharacters []string `json:"retriggerCharacters,omitempty"`
	WorkDoneProgress    bool     `json:"workDoneProgress,omitempty"`
}

type CodeLensOptions struct {
	ResolveProvider bool `json:"resolveProvider,omitempty"`
}

type DocumentOnTypeFormattingOptions struct {
	FirstTriggerCharacter string   `json:"firstTriggerCharacter"`
	MoreTriggerCharacter  []string `json:"moreTriggerCharacter,omitempty"`
}

type ExecuteCommandOptions struct {
	Commands []string `json:"commands"`
}

type WorkspaceFoldersOptions struct {
	Supported           bool `json:"supported,omitempty"`
	ChangeNotifications bool `json:"changeNotifications,omitempty"`
}

type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier `json:"textDocument"`
	ContentChanges []TextDocumentContentChange     `json:"contentChanges"`
}

type TextDocumentContentChange struct {
	Range       *Range  `json:"range,omitempty"`
	RangeLength *uint32 `json:"rangeLength,omitempty"`
	Text        string  `json:"text"`
}

type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type DidSaveTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Text         *string                `json:"text,omitempty"`
}

type WillSaveWaitUntilParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type DidChangeConfigurationParams struct {
	Settings interface{} `json:"settings"`
}

type DidChangeWatchedFilesParams struct {
	Changes []FileEvent `json:"changes"`
}

type FileEvent struct {
	URI  string `json:"uri"`
	Type int    `json:"type"`
}

const (
	FileChangeTypeCreated = 1
	FileChangeTypeChanged = 2
	FileChangeTypeDeleted = 3
)

type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type Diagnostic struct {
	Range    Range          `json:"range"`
	Severity int            `json:"severity,omitempty"`
	Code     interface{}    `json:"code,omitempty"`
	Source   string         `json:"source,omitempty"`
	Message  string         `json:"message"`
	Tags     []DiagnosticTag `json:"tags,omitempty"`
}

type DiagnosticTag int

const (
	DiagnosticTagUnnecessary = 1
	DiagnosticTagDeprecated  = 2
)

type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

type CompletionItem struct {
	Label               string      `json:"label"`
	Kind                int         `json:"kind,omitempty"`
	Tags                []int       `json:"tags,omitempty"`
	Detail              string      `json:"detail,omitempty"`
	Documentation       interface{} `json:"documentation,omitempty"`
	Deprecated          bool        `json:"deprecated,omitempty"`
	Preselect           bool        `json:"preselect,omitempty"`
	SortText            string      `json:"sortText,omitempty"`
	FilterText          string      `json:"filterText,omitempty"`
	InsertText          string      `json:"insertText,omitempty"`
	InsertTextFormat    int         `json:"insertTextFormat,omitempty"`
	TextEdit            *TextEdit   `json:"textEdit,omitempty"`
	AdditionalTextEdits []TextEdit  `json:"additionalTextEdits,omitempty"`
	CommitCharacters    []string    `json:"commitCharacters,omitempty"`
	Command             *Command    `json:"command,omitempty"`
	Data                interface{} `json:"data,omitempty"`
}

type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

type Command struct {
	Title     string        `json:"title"`
	Command   string        `json:"command"`
	Arguments []interface{} `json:"arguments,omitempty"`
}

type Hover struct {
	Contents interface{} `json:"contents"`
	Range    *Range      `json:"range,omitempty"`
}

type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature int                    `json:"activeSignature,omitempty"`
	ActiveParameter int                    `json:"activeParameter,omitempty"`
}

type SignatureInformation struct {
	Label         string                 `json:"label"`
	Documentation string                 `json:"documentation,omitempty"`
	Parameters    []ParameterInformation `json:"parameters,omitempty"`
}

type ParameterInformation struct {
	Label         string `json:"label"`
	Documentation string `json:"documentation,omitempty"`
}

type LocationLink struct {
	OriginSelectionRange *Range `json:"originSelectionRange,omitempty"`
	URI                  string `json:"uri"`
	Range                Range  `json:"range"`
	TargetURI            string `json:"targetUri"`
	TargetRange          Range  `json:"targetRange"`
	TargetSelectionRange Range  `json:"targetSelectionRange"`
}

type DocumentHighlight struct {
	Range Range `json:"range"`
	Kind  int   `json:"kind,omitempty"`
}

const (
	DocumentHighlightKindText  = 1
	DocumentHighlightKindRead  = 2
	DocumentHighlightKindWrite = 3
)

type SymbolInformation struct {
	Name          string   `json:"name"`
	Kind          int      `json:"kind"`
	Tags          []int    `json:"tags,omitempty"`
	Deprecated    bool     `json:"deprecated,omitempty"`
	Location      Location `json:"location"`
	ContainerName string   `json:"containerName,omitempty"`
}

type DocumentSymbol struct {
	Name           string           `json:"name"`
	Kind           int              `json:"kind"`
	Tags           []int            `json:"tags,omitempty"`
	Deprecated     bool             `json:"deprecated,omitempty"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

type CodeAction struct {
	Title       string         `json:"title"`
	Kind        int            `json:"kind,omitempty"`
	Tags        []int          `json:"tags,omitempty"`
	Diagnostics []Diagnostic   `json:"diagnostics,omitempty"`
	IsPreferred bool           `json:"isPreferred,omitempty"`
	Disabled    *Disabled      `json:"disabled,omitempty"`
	Command     *Command       `json:"command,omitempty"`
	Edit        *WorkspaceEdit `json:"edit,omitempty"`
}

type Disabled struct {
	Reason string `json:"reason"`
}

type WorkspaceEdit struct {
	Changes         map[string][]TextEdit `json:"changes,omitempty"`
	DocumentChanges []interface{}         `json:"documentChanges,omitempty"`
}

type RenameParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	NewName      string                 `json:"newName"`
}

type FoldingRangeParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type FoldingRange struct {
	StartLine      uint32 `json:"startLine"`
	StartCharacter uint32 `json:"startCharacter,omitempty"`
	EndLine        uint32 `json:"endLine"`
	EndCharacter   uint32 `json:"endCharacter,omitempty"`
	Kind           string `json:"kind,omitempty"`
}

type ExecuteCommandParams struct {
	Command   string        `json:"command"`
	Arguments []interface{} `json:"arguments,omitempty"`
}

type ApplyWorkspaceEditParams struct {
	Label string        `json:"label,omitempty"`
	Edit  WorkspaceEdit `json:"edit"`
}

type ApplyWorkspaceEditResult struct {
	Applied bool `json:"applied"`
}

type ShowMessageParams struct {
	Type    int    `json:"type"`
	Message string `json:"message"`
}

type MessageParams struct {
	Type    int    `json:"type"`
	Message string `json:"message"`
}

type LogMessageParams struct {
	Type    int    `json:"type"`
	Message string `json:"message"`
}

type WindowShowMessageRequestParams struct {
	Type    int      `json:"type"`
	Message string   `json:"message"`
	Actions []string `json:"actions,omitempty"`
}

type CancelParams struct {
	ID interface{} `json:"id"`
}

type DocumentURI string

func (u DocumentURI) URI() string {
	return string(u)
}

type TextDocument struct {
	Item   TextDocumentItem
	AST    *parser.Program
	Text   string
	Dirty  bool
	Events []TextDocumentEvent
}

type TextDocumentEvent interface {
	Apply(*TextDocument)
}

type TextDocumentDidOpenEvent struct {
	TextDocument TextDocumentItem
}

func (e TextDocumentDidOpenEvent) Apply(doc *TextDocument) {
	doc.Text = e.TextDocument.Text
	doc.Dirty = true
}

type TextDocumentDidChangeEvent struct {
	TextDocument   VersionedTextDocumentIdentifier
	ContentChanges []TextDocumentContentChange
}

func (e TextDocumentDidChangeEvent) Apply(doc *TextDocument) {
	for _, change := range e.ContentChanges {
		if change.Range == nil {
			doc.Text = change.Text
		} else {
			doc.applyChange(change)
		}
	}
	doc.Dirty = true
}

func (doc *TextDocument) applyChange(change TextDocumentContentChange) {
	if change.Range == nil {
		doc.Text = change.Text
		return
	}
}

type TextDocumentDidCloseEvent struct {
	TextDocument TextDocumentIdentifier
}

func (e TextDocumentDidCloseEvent) Apply(doc *TextDocument) {
	doc.Dirty = false
}

type TextDocumentDidSaveEvent struct {
	TextDocument TextDocumentIdentifier
	Text         *string
}

func (e TextDocumentDidSaveEvent) Apply(doc *TextDocument) {
}

type InitializeResultData struct {
	Capabilities ServerCapabilities `json:"capabilities"`
}

type ShutdownParams struct{}

type ExitParams struct{}

type LogParams struct {
	Message string `json:"message"`
}

type DocumentSymbolParams struct {
	TextDocument     TextDocumentIdentifier `json:"textDocument"`
	WorkDoneProgress *bool                  `json:"workDoneProgress,omitempty"`
	PartialResult    *bool                  `json:"partialResult,omitempty"`
}

type WorkspaceSymbolParams struct {
	Query            string `json:"query"`
	WorkDoneProgress *bool  `json:"workDoneProgress,omitempty"`
	PartialResult    *bool  `json:"partialResult,omitempty"`
}

type DocumentFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Options      *FormattingOptions     `json:"options,omitempty"`
}

type FormattingOptions struct {
	TabSize      uint32 `json:"tabSize,omitempty"`
	InsertSpaces bool   `json:"insertSpaces,omitempty"`
}
