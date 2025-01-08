package main

import (
    "bytes"
    "context"
    "encoding/json"
    "io/ioutil"
    "log"
    "net/http"
    "os"
    "go.lsp.dev/protocol"
    "go.lsp.dev/jsonrpc2"
)

type Server struct {
    conn   jsonrpc2.Conn
    client protocol.Client
}

func (s *Server) Start() error {
    // Create a new stream for stdin/stdout communication
    stream := jsonrpc2.NewStream(NewStdioStream())
    
    // Create a new connection
    conn := jsonrpc2.NewConn(stream)
    s.conn = conn

    // Handle incoming requests
    conn.Go(context.Background(), s.handle)
    
    // Wait for connection to close
    <-conn.Done()
    return conn.Err()
}

// handle processes incoming LSP requests
func (s *Server) handle(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
    switch req.Method() {
    case protocol.MethodInitialize:
        var params protocol.InitializeParams
        if err := req.Params().UnmarshalTo(&params); err != nil {
            return err
        }
        return reply(ctx, s.handleInitialize(&params))
        
    case protocol.MethodTextDocumentCompletion:
        var params protocol.CompletionParams
        if err := req.Params().UnmarshalTo(&params); err != nil {
            return err
        }
        return reply(ctx, s.handleCompletion(&params))
    }
    
    return nil
}

// handleInitialize processes the initialize request
func (s *Server) handleInitialize(params *protocol.InitializeParams) (*protocol.InitializeResult, error) {
    return &protocol.InitializeResult{
        Capabilities: protocol.ServerCapabilities{
            CompletionProvider: &protocol.CompletionOptions{
                TriggerCharacters: []string{".", " "},
            },
            TextDocumentSync: &protocol.TextDocumentSyncOptions{
                Change:    protocol.TextDocumentSyncKindFull,
                OpenClose: true,
            },
        },
    }, nil
}

// StdioStream implements jsonrpc2.Stream interface for stdin/stdout
type StdioStream struct {
    in  *os.File
    out *os.File
}

func NewStdioStream() *StdioStream {
    return &StdioStream{
        in:  os.Stdin,
        out: os.Stdout,
    }
}

func (s *StdioStream) Read(p []byte) (int, error) {
    return s.in.Read(p)
}

func (s *StdioStream) Write(p []byte) (int, error) {
    return s.out.Write(p)
}

func (s *StdioStream) Close() error {
    if err := s.in.Close(); err != nil {
        return err
    }
    return s.out.Close()
}

func main() {
    server := &Server{}
    if err := server.Start(); err != nil {
        log.Fatal(err)
    }
}

type OmniSharpClient struct {
    baseURL string
    client  *http.Client
}

func NewOmniSharpClient(baseURL string) *OmniSharpClient {
    return &OmniSharpClient{
        baseURL: baseURL,
        client:  &http.Client{},
    }
}

func (o *OmniSharpClient) SendRequest(endpoint string, request interface{}) ([]byte, error) {
    jsonData, err := json.Marshal(request)
    if err != nil {
        return nil, err
    }

    req, err := http.NewRequest("POST", o.baseURL+endpoint, bytes.NewBuffer(jsonData))
    if err != nil {
        return nil, err
    }

    req.Header.Set("Content-Type", "application/json")
    
    resp, err := o.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    return ioutil.ReadAll(resp.Body)
}

// Add OmniSharp client to Server struct
type Server struct {
    conn      jsonrpc2.Conn
    client    protocol.Client
    omnisharp *OmniSharpClient
}

// Update handleCompletion to use OmniSharp
func (s *Server) handleCompletion(params *protocol.CompletionParams) (*protocol.CompletionList, error) {
    // Convert LSP completion params to OmniSharp format
    omnisharpRequest := map[string]interface{}{
        "Line":     params.Position.Line,
        "Column":   params.Position.Character,
        "FileName": params.TextDocument.URI.SpanURI().Filename(),
    }

    response, err := s.omnisharp.SendRequest("/autocomplete", omnisharpRequest)
    if err != nil {
        return nil, err
    }

    // Parse OmniSharp response
    var omnisharpResponse []struct {
        CompletionText  string   `json:"CompletionText"`
        DisplayText     string   `json:"DisplayText"`
        Documentation   string   `json:"Documentation"`
        Kind           string   `json:"Kind"`
    }
    
    if err := json.Unmarshal(response, &omnisharpResponse); err != nil {
        return nil, err
    }

    // Convert to LSP completion items
    items := make([]protocol.CompletionItem, len(omnisharpResponse))
    for i, item := range omnisharpResponse {
        items[i] = protocol.CompletionItem{
            Label:         item.DisplayText,
            Detail:        item.Documentation,
            Kind:         convertKind(item.Kind),
            InsertText:    item.CompletionText,
        }
    }

    return &protocol.CompletionList{
        IsIncomplete: false,
        Items:       items,
    }, nil
}

// Helper function to convert OmniSharp completion kinds to LSP completion kinds
func convertKind(omnisharpKind string) protocol.CompletionItemKind {
    switch omnisharpKind {
    case "Method":
        return protocol.CompletionItemKindMethod
    case "Property":
        return protocol.CompletionItemKindProperty
    case "Field":
        return protocol.CompletionItemKindField
    case "Class":
        return protocol.CompletionItemKindClass
    default:
        return protocol.CompletionItemKindText
    }
}
