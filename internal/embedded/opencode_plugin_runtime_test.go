package embedded

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

func TestOpenCodeResultPluginRuntimeMutatesActualReturnShapes(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("requires Bun")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "plugin.ts"), OpenCodeResultPluginContent(), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, brand.BinaryName)
	formatter := `const x=JSON.parse(await Bun.stdin.text());
await Bun.write(process.env.CAPTURE,JSON.stringify(x));
if(process.env.FAIL){process.stdout.write("RAW_MUST_NOT_ESCAPE");process.exit(1)}
if(process.env.FLOOD){process.stdout.write("x".repeat(70000));process.exit(0)}
const content=x.content;
process.stdout.write(content.length>1024?JSON.stringify({artifact_id:"a".repeat(64),exit_code:x.exit_code,bytes:content.length}):content);
`
	if err := os.WriteFile(bin, []byte("#!"+bun+"\n"+formatter), 0700); err != nil {
		t.Fatal(err)
	}
	script := `import {ToolResultBudget} from "./plugin.ts";
import assert from "node:assert/strict";
import {mkdir,writeFile} from "node:fs/promises";
const ctx={directory:process.cwd(),worktree:"/"};
const hooks=await ToolResultBudget(ctx as any);const hook=hooks["tool.execute.after"]!;
const transform=hooks["experimental.chat.messages.transform"]!;
const rawError="MCP_ERROR_ROW\n".repeat(10000);
const errorPart=()=>({id:"part1",type:"tool",tool:"mcp_large",callID:"error-call",sessionID:"s-778",state:{status:"error",input:{},error:rawError}});
const history:any={messages:[{info:{},parts:[errorPart()]}]};
await transform({},history);const firstError=history.messages[0].parts[0].state.error;assert(firstError.length<1024);
process.env.FAIL="1";await transform({},history);assert.equal(history.messages[0].parts[0].state.error,firstError);
const restarted=await ToolResultBudget(ctx as any);const reloaded:any={messages:[{info:{},parts:[errorPart()]}]};await restarted["experimental.chat.messages.transform"]!({},reloaded);assert.equal(reloaded.messages[0].parts[0].state.error,firstError);delete process.env.FAIL;
const interrupted:any={...errorPart(),callID:"interrupted",state:{status:"error",input:{},error:"cancelled",metadata:{interrupted:true,output:rawError}}};await transform({},{messages:[{info:{} as any,parts:[interrupted]}]});assert(interrupted.state.metadata.output.length<1024);assert.equal(interrupted.state.error,"cancelled");
const input={tool:"read",sessionID:"s-778",callID:"call1",args:{filePath:"file.go",offset:12,limit:30}};
const out={title:"read",output:"archive row\n".repeat(15000),metadata:{truncated:true,exit:7,custom:"preserve"}};
await hook(input,out);assert(out.output.length<1024);assert.equal(JSON.parse(out.output).exit_code,7);
const sent=JSON.parse(await Bun.file(process.env.CAPTURE).text());assert(sent.content.length>100000);assert.equal(sent.session_id,"s-778");assert.equal(sent.truncated,true);assert.equal(JSON.parse(sent.command).offset,12);assert.equal(out.metadata.custom,"preserve");
const small={title:"small",output:"exact\n\n",metadata:{}};await hook(input,small);assert.equal(small.output,"exact\n\n");
const image={type:"image",data:"unchanged",mimeType:"image/png"};const mcp:any={content:[{type:"resource",resource:{uri:"ref:original",text:"x".repeat(8000)}},{type:"text",text:"y".repeat(8000)},image],isError:true};
await hook({...input,tool:"mcp_native"},mcp);assert.equal(mcp.content.length,2);assert.equal(mcp.content[1],image);assert.equal(mcp.isError,true);
const captured=JSON.parse(await Bun.file(process.env.CAPTURE).text());assert(captured.content.includes("ref:original"));
const smallMcp:any={content:[{type:"text",text:"part one",annotations:{audience:["assistant"]}},{type:"text",text:"part two"}]};const original=JSON.stringify(smallMcp);await hook({...input,tool:"mcp_native"},smallMcp);assert.equal(JSON.stringify(smallMcp),original);
process.env.FAIL="1";const failed={title:"failed",output:"RAW_MUST_NOT_ESCAPE",metadata:{}};await hook(input,failed);assert(failed.output.includes("raw output withheld"));assert(!failed.output.includes("RAW_MUST"));delete process.env.FAIL;
process.env.FLOOD="1";const flood={title:"flood",output:"raw input",metadata:{}};await hook(input,flood);assert(flood.output.includes("raw output withheld"));delete process.env.FLOOD;
await mkdir(".opencode/tools",{recursive:true});await writeFile(".opencode/tools/exec.ts","MANAGED_HEADER");
const managed=await ToolResultBudget(ctx as any);process.env.FAIL="1";const already={title:"exec",output:"already budgeted",metadata:{}};await managed["tool.execute.after"]!({...input,tool:"exec"},already);assert.equal(already.output,"already budgeted");
const userExec={title:"user exec",output:"must be filtered",metadata:{}};await hook({...input,tool:"exec"},userExec);assert(userExec.output.includes("raw output withheld"));
console.log("PASS native callback shapes: complete >100KB, small exact, MCP/resource/images/errors, managed exec skip, user exec covered, failure/flood closed");
`
	// Keep ownership detection coupled to the same rendered managed-file marker.
	script = strings.ReplaceAll(script, "MANAGED_HEADER", OpenCodeExecToolManagedHeader())
	if err := os.WriteFile(filepath.Join(root, "check.ts"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bun, "check.ts")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"), "CAPTURE="+filepath.Join(root, "capture.json"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("plugin runtime failed: %v\n%s", err, output)
	}
	t.Log(string(output))
}
