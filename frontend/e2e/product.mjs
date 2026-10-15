import { spawn } from "node:child_process";
import { mkdirSync, openSync, writeFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const dir = resolve(root, ".cache/browser/product");
mkdirSync(dir, { recursive: true });
const log = openSync(resolve(dir, "services.log"), "a");
const vite = spawn(
  "npm",
  ["run", "dev", "--", "--host", "127.0.0.1", "--port", "9391", "--strictPort"],
  {
    cwd: resolve(root, "frontend"),
    stdio: ["ignore", log, log],
    detached: true,
  },
);
const chrome = spawn(
  "chromium",
  [
    "--headless",
    "--no-sandbox",
    "--disable-gpu",
    "--disable-background-networking",
    "--no-first-run",
    `--user-data-dir=${resolve(dir, "chrome")}`,
    "--remote-debugging-port=9392",
    "about:blank",
  ],
  { stdio: ["ignore", log, log], detached: true },
);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let ws;
try {
  async function ready(url) {
    for (let i = 0; i < 100; i++) {
      try {
        const r = await fetch(url);
        if (r.ok) return r;
      } catch {}
      await sleep(100);
    }
    throw Error("unavailable " + url);
  }
  await ready("http://127.0.0.1:9391");
  const targets = await (await ready("http://127.0.0.1:9392/json/list")).json();
  ws = new WebSocket(
    targets.find((t) => t.type === "page").webSocketDebuggerUrl,
  );
  await new Promise((r, j) => {
    ws.onopen = r;
    ws.onerror = j;
  });
  let seq = 0;
  const pending = new Map();
  const errors = [];
  function send(method, params = {}) {
    const id = ++seq;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(Error("timeout " + method)), 20000);
      pending.set(id, { resolve, reject, timer });
      ws.send(JSON.stringify({ id, method, params }));
    });
  }
  const backend = `
const blank=()=>({schemaVersion:1,templateId:'',templateVersion:0,description:'',systemPrompt:'日常助手',persona:'',model:{provider:'',baseUrl:'',name:''},allMcpServers:true,enabledMcpServers:[],allowedTools:[],allSkills:false,enabledSkills:[],contextBudget:8192,memoryEnabled:true,planEnabled:false,delegation:{enabled:false,maxDepth:0,maxConcurrency:0,maxSeconds:0},execution:{workflow:'direct',stages:[],main:{},planner:{},reviewer:{},improver:{},worker:{},workerSources:[],budget:{},review:{mode:'off'},improverEnabled:true}});
const profile=(id,name)=>({id,name,isDefault:id==='default',revision:1,data:blank(),createdAt:'2026-10-15',updatedAt:'2026-10-15'});
const state=window.__product={n:0,profiles:[profile('default','日常助手')],conversations:[],contexts:{},runs:{},messages:{},pending:[],grants:{},model:{provider:'ollama',baseUrl:'http://127.0.0.1:11434/v1',name:''},savedKey:false,approvals:[],drafts:[],skillDrafts:[],previewRemote:false};
const status=()=>({storageReady:true,runtimeReady:!!state.model.name,modelConnected:!!state.model.name,model:state.model,availableModels:['mock'],message:state.model.name?'已连接':'请连接模型'});
export const StatusService={
GetStatus:async()=>structuredClone(status()),ListCredentialStatus:async()=>[{provider:'openai',configured:state.savedKey}],SaveCredential:async(p,key)=>{if(key!=='mock-key')throw Error('unexpected key');state.savedKey=true},DeleteCredential:async()=>{state.savedKey=false},SaveModelConfig:async(m)=>{state.model=structuredClone(m);return structuredClone(status())},ListMCPEndpoints:async()=>[],
ListProfiles:async()=>structuredClone(state.profiles),ListProfileTemplates:async()=>['daily','writing','research','development'].map((id,i)=>({id,name:['日常助手','写作编辑','研究分析（试验）','开发协作（试验）'][i],data:blank(),version:1})),ProfileReferences:async()=>[],GetProfile:async(id)=>structuredClone(state.profiles.find(p=>p.id===id)),UpdateProfile:async(p)=>{const saved={...structuredClone(p),revision:p.revision+1};state.profiles=state.profiles.map(v=>v.id===p.id?saved:v);return structuredClone(saved)},CreateProfile:async(name,from)=>{const p=profile('p'+(++state.n),name||'助手副本');if(from)p.data=structuredClone(state.profiles.find(v=>v.id===from).data);state.profiles.push(p);return structuredClone(p)},CreateTemplateProfile:async(id,name)=>{const p=profile('p'+(++state.n),name||id);state.profiles.push(p);return structuredClone(p)},ArchiveProfile:async(id)=>{state.profiles=state.profiles.filter(p=>p.id!==id)},
ListConversations:async()=>structuredClone(state.conversations),GetConversation:async(id)=>structuredClone(state.conversations.find(c=>c.id===id)),CreateConversation:async(profileId,reuse,title)=>{const id='c'+(++state.n),contextId=reuse?state.conversations.find(c=>c.id===reuse).contextId:'ctx'+state.n,c={id,profileId,contextId,title:title||'新对话',kind:'normal',profileName:state.profiles.find(p=>p.id===profileId).name,profileArchived:false,createdAt:'2026-10-15',options:{webSearch:false}};state.contexts[contextId]??={id:contextId,name:c.title,policy:{approvalMode:'semi',memory:false,permissions:{unrestricted:false,workspacePaths:[],commands:[],networkHosts:[]}}};state.conversations.unshift(c);state.messages[id]=[];return structuredClone(c)},RenameConversation:async(id,title)=>{state.conversations.find(c=>c.id===id).title=title},DeleteConversation:async(id)=>{state.conversations=state.conversations.filter(c=>c.id!==id);delete state.messages[id]},SelectConversationProfile:async(id,profileId)=>{const c=state.conversations.find(c=>c.id===id);c.profileId=profileId;c.profileName=state.profiles.find(p=>p.id===profileId).name;return structuredClone(c)},
GetLocalContext:async(id)=>structuredClone(state.contexts[id]),UpdateContextPolicy:async(id,policy)=>{state.contexts[id].policy=structuredClone(policy);return structuredClone(state.contexts[id])},ListMemories:async()=>[],ListExfilGrants:async(id)=>structuredClone(state.grants[id]??[]),GrantExfil:async(id,destination,scopes)=>{state.grants[id]=[{destination,scopes}]},RevokeExfil:async(id)=>{state.grants[id]=[]},PreviewSend:async(id)=>{const c=state.conversations.find(c=>c.id===id);return state.previewRemote&&!state.grants[c.contextId]?.length?[{provider:'openai',destination:'https://custom.example',scopes:['personal','unknown'],missing:['personal','unknown']}]:[]},
ConversationSearchAvailability:async()=>({available:false,reason:'模拟能力不可用'}),ListConversationSearch:async()=>[],SetConversationOptions:async(id,options)=>{const c=state.conversations.find(c=>c.id===id);c.options=structuredClone(options);return structuredClone(c)},
ListMessages:async(id)=>structuredClone(state.messages[id]??[]),ListChatMessages:async(id)=>({messages:structuredClone(state.messages[id]??[]).map((m,i)=>({...m,seq:i+1,sessionId:id})),beforeSeq:1,hasMore:false}),ListRuns:async(id)=>structuredClone(Object.values(state.runs).filter(r=>r.sessionId===id).reverse()),ListToolCalls:async()=>[],PendingApprovals:async()=>structuredClone(state.pending),GetSessionSummary:async()=>{throw Error('none')},GetSessionPlan:async()=>{throw Error('none')},ListChildRuns:async()=>[],GetTaskGraph:async()=>({tasks:[],attempts:[],artifacts:[],coordination:{state:'complete',rounds:0},continuations:[]}),ListTaskReviews:async()=>[],ListTaskRetrospectives:async()=>[],PendingStreamText:async()=>'',
StartConversationTask:async(id,input,uploads)=>{state.lastSend={id,input,uploads};const c=state.conversations.find(c=>c.id===id),r={id:'r'+(++state.n),sessionId:id,contextId:c.contextId,profileId:c.profileId,component:'main',status:'completed',createdAt:new Date().toISOString()};state.runs[r.id]=r;c.title=input.slice(0,32)||'附件分析';state.messages[id].push({id:r.id+'u',runId:r.id,role:'user',content:input,attachments:uploads.map(u=>({name:u.name,size:10,mediaType:u.name.endsWith('.png')?'image/png':'text/plain'}))},{id:r.id+'a',runId:r.id,role:'assistant',content:'# 回答\\n\\n- 已处理材料\\n\\n| 项目 | 状态 |\\n| --- | --- |\\n| 文件 | 完成 |\\n\\n\u0060\u0060\u0060js\\nconst answer = 42;\\n\u0060\u0060\u0060\\n\\n<script>window.__unsafe=true</script>'});return {...r,status:'running'}},GetRun:async(id)=>structuredClone(state.runs[id]),CancelTask:async(id)=>{state.runs[id].status='cancelled'},
ApproveToolCall:async(id)=>{state.approvals.push(id);state.pending=state.pending.filter(c=>c.id!==id)},DenyToolCall:async(id)=>{state.denied=id;state.pending=state.pending.filter(c=>c.id!==id)},ResolveToolCall:async(id,status,result,note)=>{state.resolution={id,status,result,note};state.pending=state.pending.filter(c=>c.id!==id)},
PickFolder:async()=>'/home/mock/Documents',MessageAttachment:async()=>({name:'note.txt',mediaType:'text/plain',text:'原始附件内容'}),
ListProfileDrafts:async()=>state.drafts,ListSkillDrafts:async()=>state.skillDrafts,
};`;
  ws.onmessage = (e) => {
    const m = JSON.parse(e.data);
    if (m.id) {
      const p = pending.get(m.id);
      if (p) {
        clearTimeout(p.timer);
        pending.delete(m.id);
        m.error
          ? p.reject(Error(JSON.stringify(m.error)))
          : p.resolve(m.result);
      }
    } else if (m.method === "Fetch.requestPaused") {
      send("Fetch.fulfillRequest", {
        requestId: m.params.requestId,
        responseCode: 200,
        responseHeaders: [
          { name: "Content-Type", value: "application/javascript" },
        ],
        body: Buffer.from(backend).toString("base64"),
      }).catch((e) => errors.push(e.message));
    } else if (m.method === "Runtime.exceptionThrown")
      errors.push(
        m.params.exceptionDetails.exception?.description ??
          m.params.exceptionDetails.text,
      );
  };
  await send("Runtime.enable");
  await send("Page.enable");
  await send("Network.enable");
  await send("Network.setCacheDisabled", { cacheDisabled: true });
  await send("Page.addScriptToEvaluateOnNewDocument", {
    source: "try{localStorage.clear();sessionStorage.clear()}catch{}",
  });
  await send("Fetch.enable", {
    patterns: [
      { urlPattern: "*/bindings/icepick/index.*", requestStage: "Request" },
    ],
  });
  await send("Emulation.setDeviceMetricsOverride", {
    width: 1366,
    height: 850,
    deviceScaleFactor: 1,
    mobile: false,
  });
  await send("Page.navigate", { url: "http://127.0.0.1:9391" });
  async function evaluate(expression) {
    const r = await send("Runtime.evaluate", {
      expression,
      returnByValue: true,
      awaitPromise: true,
    });
    if (r.exceptionDetails)
      throw Error(
        r.exceptionDetails.exception?.description ?? r.exceptionDetails.text,
      );
    return r.result.value;
  }
  async function until(expr) {
    for (let i = 0; i < 140; i++) {
      if (await evaluate(`Boolean(${expr})`)) return;
      await sleep(100);
    }
    throw Error("not reached: " + expr);
  }
  async function click(text) {
    await until(
      `Array.from(document.querySelectorAll('button')).some(e=>e.textContent.trim()===${JSON.stringify(text)}&&!e.disabled)`,
    );
    await evaluate(
      `(()=>{const b=Array.from(document.querySelectorAll('button')).find(e=>e.textContent.trim()===${JSON.stringify(text)});if(!b)throw Error('missing button '+${JSON.stringify(text)});b.click()})()`,
    );
  }
  async function fill(selector, value) {
    await evaluate(
      `(()=>{const t=document.querySelector(${JSON.stringify(selector)});if(!t)throw Error('missing input');Object.getOwnPropertyDescriptor(t instanceof HTMLTextAreaElement?HTMLTextAreaElement.prototype:HTMLInputElement.prototype,'value').set.call(t,${JSON.stringify(value)});t.dispatchEvent(new Event('input',{bubbles:true}))})()`,
    );
  }
  await until(
    `window.__product?.conversations.length===1 && document.querySelector('[aria-label="发送消息"]')`,
  );
  if (
    await evaluate(
      `/AGENT HARNESS|MCP 服务|Profiles|本地 Agent 宿主/.test(document.body.innerText)`,
    )
  )
    throw Error("runtime jargon on home");
  await click("连接模型");
  await until(`document.querySelector('.model-form')`);
  await evaluate(
    `(()=>{const s=document.querySelector('.model-form select');Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype,'value').set.call(s,'openai');s.dispatchEvent(new Event('change',{bubbles:true}))})()`,
  );
  await until(`document.querySelector('input[type=password]')`);
  await fill("input[type=password]", "mock-key");
  await fill('input[list="available-models"]', "mock");
  await click("保存并连接");
  await until(`document.body.innerText.includes('模型已连接')`);
  if (!(await evaluate(`window.__product.savedKey`)))
    throw Error("Unified model setup did not save credentials");
  await click("开始聊天 →");
  await fill('[aria-label="发送消息"]', "保留未发送的内容");
  const first = await evaluate(`window.__product.conversations[0].id`);
  await click("＋ 新对话");
  await until(
    `window.__product.conversations.length===2 && document.querySelector('[aria-label="发送消息"]').value===''`,
  );
  await evaluate(
    `Array.from(document.querySelectorAll('.session-link'))[1].click()`,
  );
  await until(
    `document.querySelector('[aria-label="发送消息"]').value==='保留未发送的内容'`,
  );
  await evaluate(
    `(()=>{const d=new DataTransfer();d.items.add(new File(['note body'],'note.txt',{type:'text/plain'}));const input=document.querySelector('[aria-label="添加对话附件"]');input.files=d.files;input.dispatchEvent(new Event('change',{bubbles:true}))})()`,
  );
  await until(
    `document.querySelector('.draft-attachments')?.innerText.includes('note.txt')`,
  );
  await evaluate(
    `(()=>{const dt=new DataTransfer();dt.items.add(new File([Uint8Array.from(atob('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jY9sAAAAASUVORK5CYII='),c=>c.charCodeAt(0))],'pasted.png',{type:'image/png'}));document.querySelector('[aria-label="发送消息"]').dispatchEvent(new ClipboardEvent('paste',{clipboardData:dt,bubbles:true,cancelable:true}))})()`,
  );
  await until(`document.querySelector('.attachment-thumb')?.naturalWidth===1`);
  await evaluate(
    `(()=>{const dt=new DataTransfer();dt.items.add(new File(['drop body'],'dropped.txt',{type:'text/plain'}));document.querySelector('.composer').dispatchEvent(new DragEvent('drop',{dataTransfer:dt,bubbles:true,cancelable:true}))})()`,
  );
  await until(
    `document.querySelector('.draft-attachments').innerText.includes('dropped.txt')`,
  );
  // Enter submits; a composing Enter must not submit.
  await evaluate(
    `document.querySelector('[aria-label="发送消息"]').dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',isComposing:true,bubbles:true}))`,
  );
  if (await evaluate(`!!window.__product.lastSend`))
    throw Error("IME submitted");
  await evaluate(
    `document.querySelector('[aria-label="发送消息"]').dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true,cancelable:true}))`,
  );
  await until(
    `document.querySelector('.markdown h1') && document.querySelector('.markdown table') && document.querySelector('.code-block')`,
  );
  if (await evaluate(`!!window.__unsafe`))
    throw Error("unsafe markdown executed");
  await until(`!document.querySelector('.draft-attachments')`);
  await click("note.txt");
  await until(
    `document.querySelector('.attachment-preview')?.innerText.includes('原始附件内容')`,
  );
  await click("关闭预览");
  // Unsupported search must offer a usable plain-chat choice, not disable send.
  await evaluate(`document.querySelector('.search-toggle input').click()`);
  await fill('[aria-label="发送消息"]', "不要联网也可以回答");
  await click("发送 ↑");
  await until(
    `document.querySelector('dialog[open]')?.innerText.includes('当前模型暂不支持联网')`,
  );
  await click("关闭联网并发送");
  await until(`window.__product.lastSend.input==='不要联网也可以回答'`);
  await sleep(1100);
  // Concrete destination consent; cancellation retains the draft and sends zero data.
  await evaluate(`window.__product.previewRemote=true`);
  await fill('[aria-label="发送消息"]', "需要授权的消息");
  await click("发送 ↑");
  await until(
    `document.querySelector('dialog[open]')?.innerText.includes('https://custom.example')`,
  );
  await click("暂不发送");
  await until(`!document.querySelector('dialog[open]')`);
  if (await evaluate(`window.__product.lastSend.input==='需要授权的消息'`))
    throw Error("send without consent");
  await click("发送 ↑");
  await until(`document.querySelector('dialog[open]')`);
  await click("允许并继续");
  await until(`window.__product.lastSend.input==='需要授权的消息'`);
  await sleep(1100);
  // Folder picker grants only after an explicit, scoped confirmation.
  await click("选择文件夹");
  await until(
    `document.querySelector('dialog[open]')?.innerText.includes('/home/mock/Documents')`,
  );
  await click("允许并添加到消息");
  await until(
    `document.querySelector('[aria-label="发送消息"]').value.includes('/home/mock/Documents')`,
  );
  // A hidden worker request is surfaced in the main chat, with action language.
  await evaluate(
    `window.__product.pending=[{id:'child-call',runId:'hidden-child',toolName:'local__write_file',arguments:JSON.stringify({path:'/home/mock/Documents/report.md',content:'报告'}),state:'pending'}]`,
  );
  await fill('[aria-label="发送消息"]', "触发审批同步");
  await click("发送 ↑");
  await until(`document.body.innerText.includes('ICEPick 想写入文件')`);
  await click("允许这次操作");
  await until(`window.__product.approvals.includes('child-call')`);
  await evaluate(
    `window.__product.pending=[{id:'command-call',runId:'child',toolName:'local__exec',arguments:JSON.stringify({executable:'/bin/echo',args:['hello'],cwd:'/home/mock'}),state:'pending'},{id:'unknown-call',runId:'child',toolName:'local__write_file',arguments:JSON.stringify({path:'/home/mock/report.md',content:'report'}),state:'execution_unknown'}]`,
  );
  await fill('[aria-label="发送消息"]', "检查审批与恢复");
  await click("发送 ↑");
  await until(
    `document.body.innerText.includes('/bin/echo') && document.body.innerText.includes('hello')`,
  );
  await click("拒绝");
  await until(
    `document.querySelector('dialog[open]')?.innerText.includes('拒绝这次操作')`,
  );
  await click("取消");
  if (await evaluate(`!!window.__product.denied`))
    throw Error("Cancel still denied tool");
  await click("拒绝");
  await until(`document.querySelector('dialog[open] textarea')`);
  await fill("dialog[open] textarea", "这次不用执行");
  await click("拒绝操作");
  await until(`window.__product.denied==='command-call'`);
  await click("暂不确定");
  if (await evaluate(`!!window.__product.resolution`))
    throw Error("Uncertain result was resolved");
  await click("已核实未完成");
  await until(`document.querySelector('dialog[open] textarea')`);
  await fill("dialog[open] textarea", "我检查了目标文件，文件尚未生成。");
  await click("记录核对结论");
  await until(`window.__product.resolution?.status==='failed'`);
  await click("◇ 助手");
  await until(`document.querySelector('.template-grid')`);
  await fill(".model-form input[required]", "未保存的助手名称");
  await click("返回对话");
  await click("◇ 助手");
  await until(
    `document.querySelector('.model-form input[required]').value==='未保存的助手名称'`,
  );
  await click("保存助手");
  await until(`window.__product.profiles[0].name==='未保存的助手名称'`);
  // Normal navigation does not change which assistant an existing chat uses.
  await click("返回对话");
  await until(`document.querySelector('.chat-page')`);
  await evaluate(
    `document.querySelector('.conversation-menu summary').click()`,
  );
  await click("重命名");
  await until(`document.querySelector('dialog[open] textarea')`);
  await fill("dialog[open] textarea", "资料整理");
  await click("保存");
  await until(
    `document.querySelector('.session-link').textContent==='资料整理'`,
  );
  await fill(".conversation-search input", "资料整理");
  await until(`document.querySelectorAll('.session-row').length===1`);
  await fill(".conversation-search input", "");
  await evaluate(
    `document.querySelector('.conversation-menu summary').click()`,
  );
  await click("删除对话");
  await until(
    `document.querySelector('dialog[open]')?.innerText.includes('无法恢复')`,
  );
  await click("取消");
  if ((await evaluate(`window.__product.conversations.length`)) !== 2)
    throw Error("Cancelled deletion removed conversation");
  await evaluate(
    `document.querySelector('.conversation-menu summary').click()`,
  );
  await click("删除对话");
  await until(`document.querySelector('dialog[open]')`);
  await evaluate(
    `Array.from(document.querySelectorAll('dialog[open] button')).find(b=>b.textContent.trim()==='删除对话').click()`,
  );
  await until(`window.__product.conversations.length===1`);
  await send("Page.captureScreenshot", { format: "png" }).then((r) =>
    writeFileSync(resolve(dir, "chat.png"), Buffer.from(r.data, "base64")),
  );
  await send("Emulation.setDeviceMetricsOverride", {
    width: 800,
    height: 600,
    deviceScaleFactor: 1,
    mobile: false,
  });
  if (await evaluate(`document.documentElement.scrollWidth>innerWidth`))
    throw Error("horizontal overflow");
  if (
    await evaluate(
      `document.querySelector('.message-list').scrollHeight>document.querySelector('.message-list').clientHeight+1`,
    )
  )
    throw Error("nested message scroll");
  if (errors.length) throw Error(errors.join("\n"));
  console.log(
    "PASS: default chat, model connection, preserved drafts, file upload/preview, Markdown/table/code and HTML safety, IME/Enter, search recovery, explicit endpoint consent, folder permission, child approval aggregation, assistant editing, conversation rename/search/delete, drag/paste thumbnails, guided unknown outcome and cancellable denial, narrow layout and single chat scroll.",
  );
} finally {
  try {
    ws?.close();
  } catch {}
  for (const child of [vite, chrome]) {
    try {
      process.kill(-child.pid, "SIGTERM");
    } catch {}
  }
}
