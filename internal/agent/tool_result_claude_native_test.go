package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
	for _, sample := range []struct{ name, script string }{{"native", claudeNativeProbeScript}, {"lifecycle", claudeLifecycleProbeScript}, {"mcp", claudeMCPProbeScript}, {"mcp_failure", claudeMCPProbeScript}} {
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
				for _, id := range []string{"toolu_big", "toolu_fail", "toolu_read", "toolu_grep"} {
					content, _ := json.Marshal(results[id])
					if !strings.Contains(string(content), "artifact_id") {
						t.Fatalf("%s missing native digest: %s", id, content)
					}
				}
				if results["toolu_fail"]["is_error"] != true {
					t.Fatal("original Bash failure status lost")
				}
			} else if sample.name == "mcp" {
				content, _ := json.Marshal(results["toolu_mcp"])
				if !strings.Contains(string(content), "artifact_id") {
					t.Fatalf("MCP result not rewritten: %s", content)
				}
			} else {
				poll, _ := json.Marshal(results["toolu_poll"])
				if !strings.Contains(string(poll), "artifact_id") {
					t.Fatal("background TaskOutput did not return digest")
				}
				cwd, _ := json.Marshal(results["toolu_checkcwd"])
				if !strings.Contains(string(cwd), "/subdir|") {
					t.Fatal("native cwd semantics regressed")
				}
				timeout, _ := json.Marshal(results["toolu_timeout"])
				if !strings.Contains(strings.ToLower(string(timeout)), "background") {
					t.Fatalf("expected native timeout auto-background: %s", timeout)
				}
				detached, _ := json.Marshal(results["toolu_detached"])
				if !strings.Contains(string(detached), "DETACHED_DONE") {
					t.Fatal("detached shell background command did not return")
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
  n=len(calls); body=[{'type': 'tool_use', 'id': 'toolu_big', 'name': 'Bash', 'input': {'command': 'cat __ROOT__/fixture.txt'}}, {'type': 'tool_use', 'id': 'toolu_fail', 'name': 'Bash', 'input': {'command': 'cat __ROOT__/fixture.txt; exit 7'}}, {'type': 'tool_use', 'id': 'toolu_read', 'name': 'Read', 'input': {'file_path': '__ROOT__/fixture.txt'}}, {'type': 'tool_use', 'id': 'toolu_grep', 'name': 'Grep', 'input': {'pattern': 'LARGE_FIXTURE', 'path': '__ROOT__/fixture.txt', 'output_mode': 'content'}}, {'type': 'tool_use', 'id': 'toolu_glob', 'name': 'Glob', 'input': {'pattern': '*', 'path': '__ROOT__'}}] if n==1 else [{'type':'text','text':'MOCK_FINISHED'}]
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

const claudeLifecycleProbeScript = `import json,os,threading,subprocess,pathlib
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
  n=len(calls); body=([{'type':'tool_use','id':'toolu_bg','name':'Bash','input':{'command':'sleep 0.5; cat __ROOT__/fixture.txt','run_in_background':True}}] if n==1 else [{'type':'tool_use','id':'toolu_poll','name':'TaskOutput','input':{'task_id':__import__('re').search(r'(?:ID: |task_id["\s:=]+)([a-zA-Z0-9_-]+)',json.dumps(v['messages'])).group(1),'block':True,'timeout':30000}}] if n==2 else [{'type':'tool_use','id':'toolu_timeout','name':'Bash','input':{'command':'cat __ROOT__/fixture.txt; sleep 2','timeout':100}}] if n==3 else [{'type':'tool_use','id':'toolu_cwd','name':'Bash','input':{'command':'cd __ROOT__/subdir; export DEV778_EXPORTED=local_only; printf CHANGED'}}] if n==4 else [{'type':'tool_use','id':'toolu_checkcwd','name':'Bash','input':{'command':'printf "CURRENT:%s|%s" "$PWD" "$DEV778_EXPORTED"'}}] if n==5 else [{'type':'tool_use','id':'toolu_detached','name':'Bash','input':{'command':'sleep 2 & printf DETACHED_DONE'}}] if n==6 else [{'type':'text','text':'MOCK_FINISHED'}])
  msg={'id':f'msg_mock_{n}','type':'message','role':'assistant','model':'claude-sonnet-4-6','content':body,'stop_reason':'tool_use' if n<=6 else 'end_turn','stop_sequence':None,'usage':{'input_tokens':100,'output_tokens':10}}
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
a=['claude','-p','--dangerously-skip-permissions','--setting-sources','','--settings',str(p/'settings.json'),'--strict-mcp-config','--mcp-config','{"mcpServers":{}}','--tools','Bash,Read,Grep,Glob,TaskOutput','--system-prompt','Test tool result transformation.','--model','claude-sonnet-4-6','--no-session-persistence','--output-format','stream-json','--verbose','Read the fixture using both tools and finish.']
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
