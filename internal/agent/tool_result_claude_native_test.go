package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
)

// Opt-in because this executes the installed Claude binary. The Messages API
// is deterministic localhost-only, with a FAKE OAuth token: no model credits,
// live credentials, network provider inference or global settings changes.
// The branded TEST_BINARY variable must name the real compiled CLI candidate
// under review.
func TestClaudeToolResultNativeBoundary(t *testing.T) {
	if os.Getenv("CLAUDE_TOOL_RESULT_NATIVE") != "1" {
		t.Skip("set CLAUDE_TOOL_RESULT_NATIVE=1 and " + brand.EnvName("TEST_BINARY") + " to exercise installed Claude")
	}
	binary := os.Getenv(brand.EnvName("TEST_BINARY"))
	if binary == "" {
		t.Fatal(brand.EnvName("TEST_BINARY") + " required")
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct{ name, script string }{{"native", claudeNativeProbeScript}, {"mcp", claudeMCPProbeScript}, {"mcp_failure", claudeMCPProbeScript}} {
		t.Run(sample.name, func(t *testing.T) {
			root := t.TempDir()
			if err := validateClaudeToolResultBoundary(context.Background(), "claude", root, nil); err != nil {
				t.Fatal(err)
			}
			settings, err := ClaudeToolResultSettings(binary, root)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte(settings), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "fixture.txt"), []byte(strings.Repeat("LARGE_FIXTURE_CONTENT_8fc231 "+strings.Repeat("x", 60)+"\n", 1600)), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(root, "subdir"), 0700); err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(sample.name, "mcp") {
				fixture := claudeMCPFixtureScript
				if sample.name == "mcp_failure" {
					fixture = strings.ReplaceAll(fixture, "False", "True")
				}
				if err := os.WriteFile(filepath.Join(root, "mcp.py"), []byte(fixture), 0600); err != nil {
					t.Fatal(err)
				}
			}
			script := strings.ReplaceAll(sample.script, "__ROOT__", root)
			path := filepath.Join(root, "probe.py")
			if err := os.WriteFile(path, []byte(script), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "python3", path)
			cmd.Env = append(os.Environ(), brand.EnvName("TOOL_RESULT_THRESHOLD_BYTES")+"=4096", brand.EnvName("TOOL_RESULT_DIGEST_BYTES")+"=2048")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("native probe failed: %v\n%s", err, output)
			}
			data, err := os.ReadFile(filepath.Join(root, "model_requests.json"))
			if err != nil {
				t.Fatal(err)
			}
			var requests []struct {
				Messages []struct {
					Content any `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(data, &requests); err != nil {
				t.Fatal(err)
			}
			if sample.name == "mcp_failure" {
				if len(requests) != 1 {
					t.Fatalf("failed MCP output reached another model request: %d", len(requests))
				}
				artifacts, err := filepath.Glob(filepath.Join(root, paths.ProjectDirName(), "tool-results", "*.txt"))
				if err != nil || len(artifacts) == 0 {
					t.Fatalf("no full failed MCP artifact: %v", err)
				}
				content, err := os.ReadFile(artifacts[0])
				if err != nil || strings.Count(string(content), "MCP_PRIVATE_8fc231") < 100 {
					t.Fatalf("failed MCP artifact missing available (provider-truncated) error: bytes=%d err=%v", len(content), err)
				}
				events, err := filepath.Glob(filepath.Join(root, paths.ProjectDirName(), "tool-results", "events", "*.json"))
				if err != nil || len(events) != 1 {
					t.Fatalf("failed MCP must create exactly one telemetry event: events=%d err=%v", len(events), err)
				}
				t.Logf("failed MCP persisted %d bytes and one event; next model request prevented", len(content))
				return
			}
			if len(requests) < 2 {
				t.Fatal("no post-tool model request observed")
			}
			results := map[string]map[string]any{}
			for _, message := range requests[len(requests)-1].Messages {
				blocks, _ := message.Content.([]any)
				for _, value := range blocks {
					block, _ := value.(map[string]any)
					if block["type"] == "tool_result" {
						id, _ := block["tool_use_id"].(string)
						results[id] = block
					}
				}
			}
			for id, result := range results {
				content, _ := json.Marshal(result["content"])
				t.Logf("%s model-facing result bytes=%d is_error=%v", id, len(content), result["is_error"])
				if strings.Count(string(content), "LARGE_FIXTURE_CONTENT_8fc231") > 50 {
					t.Fatalf("raw oversized output reached model for %s", id)
				}
			}
			if sample.name == "native" {
				for _, id := range []string{"toolu_read", "toolu_grep"} {
					content, _ := json.Marshal(results[id])
					if !strings.Contains(string(content), "artifact_id") {
						t.Fatalf("%s missing native digest: %s", id, content)
					}
				}
			} else {
				content, _ := json.Marshal(results["toolu_mcp"])
				if !strings.Contains(string(content), "artifact_id") {
					t.Fatalf("MCP result not rewritten: %s", content)
				}
			}
		})
	}
}

// Compare the actual client's permission decisions, rather than inferring them
// from hook JSON. The dontAsk baseline must deny arbitrary Python without an
// allow; bypass cases check that the boundary keeps deny rules matching the
// original command, which a Bash input rewrite defeated (ADR-0178).
func TestClaudeToolResultNativePermissions(t *testing.T) {
	if os.Getenv("CLAUDE_TOOL_RESULT_NATIVE") != "1" {
		t.Skip("set CLAUDE_TOOL_RESULT_NATIVE=1 to exercise installed Claude")
	}
	binary := os.Getenv(brand.EnvName("TEST_BINARY"))
	if binary == "" {
		t.Fatal(brand.EnvName("TEST_BINARY") + " required")
	}
	command := `python3 -c 'from pathlib import Path; Path("executed").write_text("ORIGINAL_EXECUTED"); print("ORIGINAL_EXECUTED")'`
	for _, sample := range []struct {
		name, rule, decision string
		allowed, bypass      bool
	}{
		{"no_rule", "", "", false, false},
		{"native_allow", "allow", "", true, false},
		{"native_deny", "deny", "", false, false},
		{"native_ask", "ask", "", false, false},
		{"policy_allow", "", "allow", true, false},
		{"policy_manual", "", "ask", false, false},
		{"policy_deny", "", "deny", false, false},
		{"policy_error", "", "error", false, false},
		{"native_deny_policy_allow", "deny", "allow", false, false},
		{"native_ask_policy_allow", "ask", "allow", false, false},
		{"bypass_no_rule", "", "", true, true},
		{"bypass_native_deny", "deny", "", false, true},
		{"bypass_native_deny_policy_allow", "deny", "allow", false, true},
		{"bypass_policy_deny", "", "deny", false, true},
	} {
		t.Run(sample.name, func(t *testing.T) {
			baselineReturnedMarker := false
			for _, capture := range []bool{false, true} {
				root := t.TempDir()
				settings := map[string]any{}
				if capture {
					overlay, err := ClaudeToolResultSettings(binary, root)
					if err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal([]byte(overlay), &settings); err != nil {
						t.Fatal(err)
					}
				}
				if sample.rule != "" {
					settings["permissions"] = map[string]any{sample.rule: []string{"Bash(" + command + ")"}}
				}
				if sample.decision != "" {
					policy := "import json,sys\njson.load(sys.stdin)\n"
					if sample.decision == "error" {
						policy += "sys.stderr.write('fixture policy refused command')\nsys.exit(2)\n"
					} else {
						verdict, err := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse", "permissionDecision": sample.decision, "permissionDecisionReason": "fixture policy"}})
						if err != nil {
							t.Fatal(err)
						}
						policy += "print(" + strconv.Quote(string(verdict)) + ")\n"
					}
					policyPath := filepath.Join(root, "policy.py")
					if err := os.WriteFile(policyPath, []byte(policy), 0600); err != nil {
						t.Fatal(err)
					}
					hooks, _ := settings["hooks"].(map[string]any)
					if hooks == nil {
						hooks = map[string]any{}
						settings["hooks"] = hooks
					}
					pre, _ := hooks["PreToolUse"].([]any)
					hooks["PreToolUse"] = append(pre, map[string]any{"matcher": "Bash", "hooks": []any{map[string]any{"type": "command", "command": "python3 " + shellQuoteForTest(policyPath)}}})
				}
				config, err := json.Marshal(settings)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "settings.json"), config, 0600); err != nil {
					t.Fatal(err)
				}
				probe, err := json.Marshal(map[string]string{"command": command})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "permission-probe.json"), probe, 0600); err != nil {
					t.Fatal(err)
				}
				script := strings.ReplaceAll(claudeNativeProbeScript, "__ROOT__", root)
				if !sample.bypass {
					script = strings.Replace(script, "'--dangerously-skip-permissions'", "'--permission-mode','dontAsk'", 1)
				}
				script = strings.Replace(script, "e=os.environ.copy()", "e={k:os.environ[k] for k in ['PATH','HOME','TMPDIR','LANG'] if k in os.environ}", 1)
				path := filepath.Join(root, "probe.py")
				if err := os.WriteFile(path, []byte(script), 0600); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				output, err := exec.CommandContext(ctx, "python3", path).CombinedOutput()
				cancel()
				if err != nil {
					t.Fatalf("capture=%v native probe failed: %v\n%s", capture, err, output)
				}
				data, err := os.ReadFile(filepath.Join(root, "model_requests.json"))
				if err != nil {
					t.Fatal(err)
				}
				var requests []struct {
					Messages []struct {
						Content json.RawMessage `json:"content"`
					} `json:"messages"`
				}
				if err := json.Unmarshal(data, &requests); err != nil || len(requests) != 2 {
					t.Fatalf("capture=%v expected one tool call and next model request: requests=%d err=%v", capture, len(requests), err)
				}
				found, denied, returnedMarker := false, false, false
				for _, message := range requests[1].Messages {
					var blocks []map[string]any
					if json.Unmarshal(message.Content, &blocks) != nil {
						continue // The initial user message may be a string.
					}
					for _, block := range blocks {
						if block["type"] == "tool_result" && block["tool_use_id"] == "toolu_permission" {
							found, denied = true, block["is_error"] == true
							content, err := json.Marshal(block["content"])
							if err != nil {
								t.Fatal(err)
							}
							returnedMarker = strings.Contains(string(content), "ORIGINAL_EXECUTED")
						}
					}
				}
				marker, err := os.ReadFile(filepath.Join(root, "executed"))
				if err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				executed := string(marker) == "ORIGINAL_EXECUTED"
				if !capture {
					baselineReturnedMarker = returnedMarker
				}
				t.Logf("capture=%v executed=%v denied=%v returned_marker=%v", capture, executed, denied, returnedMarker)
				if !found || executed != sample.allowed || denied == sample.allowed || (sample.allowed && !returnedMarker) || returnedMarker != baselineReturnedMarker {
					t.Errorf("capture=%v original-command permission parity failed: expected allowed=%v found=%v executed=%v denied=%v returned_marker=%v", capture, sample.allowed, found, executed, denied, returnedMarker)
				}
			}
		})
	}
}

const claudeNativeProbeScript = `import json,os,threading,subprocess,pathlib
from http.server import BaseHTTPRequestHandler,HTTPServer
p=pathlib.Path('__ROOT__'); calls=[]
class H(BaseHTTPRequestHandler):
 def log_message(self,*a):pass
 def do_GET(self):
  self.send_response(200);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(b'{"data":[]}')
 def do_POST(self):
  v=json.loads(self.rfile.read(int(self.headers.get('Content-Length',0))))
  if '/count_tokens' in self.path:
   self.send_response(200);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(b'{"input_tokens":100}');return
  calls.append(v); (p/'model_requests.json').write_text(json.dumps(calls))
  n=len(calls); body=[{'type': 'tool_use', 'id': 'toolu_read', 'name': 'Read', 'input': {'file_path': '__ROOT__/fixture.txt'}}, {'type': 'tool_use', 'id': 'toolu_grep', 'name': 'Grep', 'input': {'pattern': 'LARGE_FIXTURE', 'path': '__ROOT__/fixture.txt', 'output_mode': 'content'}}, {'type': 'tool_use', 'id': 'toolu_glob', 'name': 'Glob', 'input': {'pattern': '*', 'path': '__ROOT__'}}] if n==1 else [{'type':'text','text':'MOCK_FINISHED'}]
  if (p/'permission-probe.json').exists():
   probe=json.loads((p/'permission-probe.json').read_text())
   body=[{'type':'tool_use','id':'toolu_permission','name':'Bash','input':probe}] if n==1 else [{'type':'text','text':'MOCK_FINISHED'}]
  msg={'id':f'msg_mock_{n}','type':'message','role':'assistant','model':'claude-sonnet-4-6','content':body,'stop_reason':'tool_use' if n==1 else 'end_turn','stop_sequence':None,'usage':{'input_tokens':100,'output_tokens':10}}
  self.send_response(200);self.send_header('Content-Type','text/event-stream' if v.get('stream') else 'application/json');self.end_headers()
  if not v.get('stream'):self.wfile.write(json.dumps(msg).encode());return
  def ev(kind,data):self.wfile.write(('event: '+kind+'\ndata: '+json.dumps({'type':kind,**data})+'\n\n').encode())
  ev('message_start',{'message':{**msg,'content':[],'stop_reason':None}})
  for i,b in enumerate(body):
   ev('content_block_start',{'index':i,'content_block':{**b,**({'input':{}} if b['type']=='tool_use' else {'text':''})}})
   ev('content_block_delta',{'index':i,'delta':{'type':'input_json_delta','partial_json':json.dumps(b['input'])} if b['type']=='tool_use' else {'type':'text_delta','text':b['text']}})
   ev('content_block_stop',{'index':i})
  ev('message_delta',{'delta':{'stop_reason':msg['stop_reason'],'stop_sequence':None},'usage':{'output_tokens':10}});ev('message_stop',{})
s=HTTPServer(('127.0.0.1',0),H);threading.Thread(target=s.serve_forever,daemon=True).start()
e=os.environ.copy();e['ANTHROPIC_BASE_URL']=f'http://127.0.0.1:{s.server_port}';e['CLAUDE_CODE_OAUTH_TOKEN']='sk-ant-oat01-dev778-local-fake-token';e.pop('ANTHROPIC_API_KEY',None)
a=['claude','-p','--dangerously-skip-permissions','--setting-sources','','--settings',str(p/'settings.json'),'--strict-mcp-config','--mcp-config','{"mcpServers":{}}','--tools','Bash,Read,Grep,Glob','--system-prompt','Test tool result transformation.','--model','claude-sonnet-4-6','--no-session-persistence','--output-format','stream-json','--verbose','Read the fixture using both tools and finish.']
with (p/'mock_stdout.jsonl').open('w') as o,(p/'mock_stderr.txt').open('w') as err:r=subprocess.run(a,cwd=p,env=e,stdout=o,stderr=err,timeout=60)
s.shutdown();print('exit',r.returncode,'requests',len(calls)); print((p/'mock_stderr.txt').read_text()[-1500:])
if len(calls)>1:
 v=json.dumps(calls[-1]['messages']);print('NEXT_REQUEST_CONTAINS:',{x:x in v for x in ['PRIVATE_FIXTURE_ORIGINAL_8fc231','REPLACEMENT_BASH_K7D92','REPLACEMENT_READ_Q8E41']})
`

const claudeMCPProbeScript = `import json,os,threading,subprocess,pathlib
from http.server import BaseHTTPRequestHandler,HTTPServer
p=pathlib.Path('__ROOT__'); calls=[]
class H(BaseHTTPRequestHandler):
 def log_message(self,*a):pass
 def do_GET(self):
  self.send_response(200);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(b'{"data":[]}')
 def do_POST(self):
  v=json.loads(self.rfile.read(int(self.headers.get('Content-Length',0))))
  if '/count_tokens' in self.path:
   self.send_response(200);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(b'{"input_tokens":100}');return
  calls.append(v); (p/'model_requests.json').write_text(json.dumps(calls))
  n=len(calls); body=[{'type': 'tool_use', 'id': 'toolu_mcp', 'name': 'mcp__fixture__lookup', 'input': {}}] if n==1 else [{'type':'text','text':'MOCK_FINISHED'}]
  msg={'id':f'msg_mock_{n}','type':'message','role':'assistant','model':'claude-sonnet-4-6','content':body,'stop_reason':'tool_use' if n==1 else 'end_turn','stop_sequence':None,'usage':{'input_tokens':100,'output_tokens':10}}
  self.send_response(200);self.send_header('Content-Type','text/event-stream' if v.get('stream') else 'application/json');self.end_headers()
  if not v.get('stream'):self.wfile.write(json.dumps(msg).encode());return
  def ev(kind,data):self.wfile.write(('event: '+kind+'\ndata: '+json.dumps({'type':kind,**data})+'\n\n').encode())
  ev('message_start',{'message':{**msg,'content':[],'stop_reason':None}})
  for i,b in enumerate(body):
   ev('content_block_start',{'index':i,'content_block':{**b,**({'input':{}} if b['type']=='tool_use' else {'text':''})}})
   ev('content_block_delta',{'index':i,'delta':{'type':'input_json_delta','partial_json':json.dumps(b['input'])} if b['type']=='tool_use' else {'type':'text_delta','text':b['text']}})
   ev('content_block_stop',{'index':i})
  ev('message_delta',{'delta':{'stop_reason':msg['stop_reason'],'stop_sequence':None},'usage':{'output_tokens':10}});ev('message_stop',{})
s=HTTPServer(('127.0.0.1',0),H);threading.Thread(target=s.serve_forever,daemon=True).start()
e=os.environ.copy();e['ANTHROPIC_BASE_URL']=f'http://127.0.0.1:{s.server_port}';e['CLAUDE_CODE_OAUTH_TOKEN']='sk-ant-oat01-dev778-local-fake-token';e.pop('ANTHROPIC_API_KEY',None)
a=['claude','-p','--dangerously-skip-permissions','--setting-sources','','--settings',str(p/'settings.json'),'--strict-mcp-config','--mcp-config','{"mcpServers": {"fixture": {"command": "python3", "args": ["__ROOT__/mcp.py"]}}}','--tools','mcp__fixture__lookup','--system-prompt','Test tool result transformation.','--model','claude-sonnet-4-6','--no-session-persistence','--output-format','stream-json','--verbose','Read the fixture using both tools and finish.']
with (p/'mock_stdout.jsonl').open('w') as o,(p/'mock_stderr.txt').open('w') as err:r=subprocess.run(a,cwd=p,env=e,stdout=o,stderr=err,timeout=60)
s.shutdown();print('exit',r.returncode,'requests',len(calls)); print((p/'mock_stderr.txt').read_text()[-1500:])
if len(calls)>1:
 v=json.dumps(calls[-1]['messages']);print('NEXT_REQUEST_CONTAINS:',{x:x in v for x in ['PRIVATE_FIXTURE_ORIGINAL_8fc231','REPLACEMENT_BASH_K7D92','REPLACEMENT_READ_Q8E41']})
`

const claudeMCPFixtureScript = `import sys,json
for line in sys.stdin:
 v=json.loads(line);m=v.get('method');ident=v.get('id')
 if ident is None:continue
 if m=='initialize':r={'protocolVersion':'2024-11-05','capabilities':{'tools':{}},'serverInfo':{'name':'fixture','version':'1'}}
 elif m=='tools/list':r={'tools':[{'name':'lookup','description':'Return fixture text','inputSchema':{'type':'object','properties':{}}}]}
 elif m=='tools/call':r={'content':[{'type':'text','text':'MCP_PRIVATE_8fc231\n'*4000}], 'isError':False}
 else:r={}
 print(json.dumps({'jsonrpc':'2.0','id':ident,'result':r}),flush=True)
`
