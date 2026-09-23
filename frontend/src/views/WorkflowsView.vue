<script setup lang="ts">
// @ts-nocheck
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import { onBeforeRouteLeave, useRoute, useRouter } from "vue-router";
import { Handle, Position, VueFlow, useVueFlow } from "@vue-flow/core";
import "@vue-flow/core/dist/style.css";
import "@vue-flow/core/dist/theme-default.css";
import {
  Archive,
  FolderOpen,
  UploadCloud,
  PackageOpen,
  Terminal,
  Webhook,
  ShieldCheck,
  Plus,
  Save,
  Undo2,
  Redo2,
  Maximize,
  Play,
  Copy,
  Trash2,
  ArrowLeft,
  Eye,
  GitBranch,
  Split,
  Repeat2,
  CircleStop,
  FileSearch,
  ServerCog,
  FolderKanban,
  History as HistoryIcon,
} from "lucide-vue-next";
import { api } from "../api";
import Modal from "../components/Modal.vue";
import Drawer from "../components/Drawer.vue";
import EmptyState from "../components/EmptyState.vue";
import FileTree from "../components/FileTree.vue";
import AIChatPanel from "../components/AIChatPanel.vue";
import RunDetailDrawer from "../components/RunDetailDrawer.vue";
const route = useRoute(),
  pid = Number(route.params.id),
  releases = ref<any[]>([]),
  connections = ref<any[]>([]),
  editing = ref<number>(),
  name = ref(""),
  enabled = ref(true),
  triggerType = ref("any"),
  triggerGlob = ref("v*"),
  nodes = ref<any[]>([]),
  edges = ref<any[]>([]),
  selected = ref<any>(),
  nodeEditorOpen = ref(false),
  selectedEdge = ref<any>(),
  createOpen = ref(false),
  triggerOpen = ref(false),
  historyOpen = ref(false),
  leaveOpen = ref(false),
  runOpen = ref(false),
  runDetailOpen = ref(false),
  runDetailID = ref<number>(),
  releaseID = ref(0),
  previewReleaseID = ref(0),
  previewFiles = ref<any[]>([]),
  previewOpen = ref(false),
  saved = ref(true),
  history = ref<string[]>([]),
  future = ref<string[]>([]),
  error = ref("");
const project=ref<any>();
const triggerDraft=ref({type:"any",glob:"v*"});
const workflowHistory=ref<any[]>([]),historyPreview=ref<any>(),baseRevisionID=ref(0);
const savedSignature=ref("");
let leaveResolver:((value:boolean)=>void)|undefined;
const canDevelop=computed(()=>['owner','admin','developer'].includes(project.value?.role));
const router = useRouter();
const { addEdges, fitView } = useVueFlow();
const modules: any = {
  archive: {
    name: "归档压缩",
    desc: "将文件打包为 ZIP 或 tar.gz",
    icon: Archive,
    defaults: { file_pattern: ".*", output: "artifact.zip", format: "zip" },
  },
  extract: {
    name: "解压文件",
    desc: "安全解压受支持的归档",
    icon: FolderOpen,
    defaults: { file_pattern: "\\.zip$", output: "extracted" },
  },
  sftp_upload: {
    name: "SFTP 上传",
    desc: "上传文件或目录到服务器",
    icon: UploadCloud,
    defaults: {
      connection_id: 0,
      file_pattern: ".*",
      destination: "/tmp/artifacts",
    },
  },
  sftp_extract: {
    name: "上传并解压",
    desc: "上传 Action 的唯一压缩包并在服务器解压",
    icon: PackageOpen,
    defaults: {
      connection_id: 0,
      destination: "/opt/application",
      permission: "0755",
      keep_archive: false,
    },
  },
  ssh_command: {
    name: "SSH 命令",
    desc: "在远程主机执行命令",
    icon: Terminal,
    defaults: { connection_id: 0, commands: ["echo deployed"], work_dir: "" },
  },
  http_webhook: {
    name: "HTTP 回调",
    desc: "调用受控 URL 通知外部服务",
    icon: Webhook,
    defaults: {
      method: "POST",
      url: "",
      body: '{"version":"{{release.version}}"}',
    },
  },
  checksum_verify: {
    name: "摘要校验",
    desc: "验证文件 SHA-256 或 SHA-512",
    icon: ShieldCheck,
    defaults: { file_pattern: "", algorithm: "sha256", expected: "" },
  },
  remote_file_exists:{name:"远端文件存在",desc:"判断服务器上是否存在指定文件",icon:FileSearch,category:"判断",defaults:{connection_id:0,path:"/opt/application/current"}},
  value_match:{name:"值匹配",desc:"判断模板值或上游输出是否匹配",icon:GitBranch,category:"判断",defaults:{actual:"",operator:"equals",expected:"",ignore_case:false}},
  string_split:{name:"字符串分割",desc:"将字符串派生为列表",icon:Split,category:"数据派生",defaults:{value:"",separator:",",regex:false,trim:true,drop_empty:true}},
  foreach:{name:"列表循环",desc:"按列表项重复执行循环子图",icon:Repeat2,category:"流程控制",defaults:{items_from:"",mode:"parallel",concurrency:4,end_node_id:""}},
  loop_end:{name:"循环结束",desc:"汇总每次循环的节点输出",icon:CircleStop,category:"流程控制",defaults:{}},
  server_list:{name:"服务器列表",desc:"选择多台服务器并批量执行范围内任务",icon:ServerCog,category:"流程控制",defaults:{connection_ids:[],mode:"parallel",concurrency:4,end_node_id:""}},
  server_list_end:{name:"服务器列表结束",desc:"汇总每台服务器的状态与节点输出",icon:CircleStop,category:"流程控制",defaults:{}},
};
for(const key of ["archive","extract","sftp_upload","sftp_extract","checksum_verify"])modules[key].category="文件操作";for(const key of ["ssh_command","http_webhook"])modules[key].category="信息交互";
const moduleGroups=computed(()=>["文件操作","流程控制","判断","数据派生","信息交互"].map(category=>({category,items:Object.entries(modules).filter(([,m]:any)=>m.category===category)})));
const persistedSignature=computed(()=>JSON.stringify({name:name.value,enabled:enabled.value,trigger_type:triggerType.value,trigger_glob:triggerGlob.value,nodes:nodes.value.map((n:any)=>({id:n.id,name:n.data.name||"",type:n.data.module,config:n.data.config,timeout_seconds:n.data.timeout_seconds,retries:n.data.retries,position:n.position})),edges:edges.value.map((e:any)=>({from:e.source,to:e.target,condition:e.condition||"success"}))}));
const triggerLabel=computed(()=>({any:"任意版本",tag:"仅 Tag",commit:"所有分支 Commit",tag_glob:`Tag ${triggerGlob.value}`,branch_glob:`分支 ${triggerGlob.value}`} as any)[triggerType.value]||triggerType.value);
function openTriggerEditor(){triggerDraft.value={type:triggerType.value,glob:triggerGlob.value||(triggerType.value==="branch_glob"?"main":"v*")};triggerOpen.value=true}
function applyTrigger(){triggerType.value=triggerDraft.value.type;triggerGlob.value=triggerDraft.value.glob.trim();saved.value=false;triggerOpen.value=false}
function normalizeNodeConfig(type:string, source:any) {
  const c={...(source||{})};
  if(type==="sftp_upload"){if(c.destination===undefined)c.destination=c.remote_dir;if(c.file_pattern===undefined)c.file_pattern=c.local_path;}
  if(type==="extract"){if(c.output===undefined)c.output=c.target_dir;if(c.file_pattern===undefined)c.file_pattern=c.archive_dir;}
  if(type==="ssh_command"){if(!Array.isArray(c.commands)&&c.command)c.commands=[c.command];if(c.work_dir===undefined)c.work_dir=c.workdir||"";}
  return {...(modules[type]?.defaults||{}),...c};
}
async function load() {
  project.value=await api(`/api/projects/${pid}`);connections.value=await api(`/api/ssh-connections?project_id=${pid}`);releases.value=project.value.type==='artifact'?await api(`/api/projects/${pid}/releases`):[];
}
async function loadPreview() {
  if (!previewReleaseID.value) {
    previewFiles.value = [];
    return;
  }
  const release = await api<any>(
    `/api/projects/${pid}/releases/${previewReleaseID.value}`,
  );
  previewFiles.value = release.files || [];
}
function openPreview() {
  selected.value = undefined;
  previewOpen.value = true;
}
const patternResult = computed(() => {
  const expression = selected.value?.data?.config?.file_pattern || "";
  if (!expression) return { files: previewFiles.value, error: "" };
  try {
    const regex = new RegExp(expression);
    return {
      files: previewFiles.value.filter((file) => regex.test(workspaceFileName(file)) || regex.test(file.name)),
      error: "",
    };
  } catch (e: any) {
    return { files: [], error: e.message };
  }
});
function workspaceFileName(file: any) {
  return `${file.kind === "extracted" ? "files" : "archive"}/${String(file.name || "").replaceAll("\\", "/")}`;
}
const supportsPattern = computed(() =>
  ["archive", "extract", "sftp_upload", "checksum_verify"].includes(
    selected.value?.data?.module,
  ),
);
function selectExactFile(file: any) {
  const escaped = workspaceFileName(file).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  selected.value.data.config.file_pattern = `^${escaped}$`;
}
function snapshot() {
  return JSON.stringify({ nodes: nodes.value, edges: edges.value });
}
function checkpoint() {
  history.value.push(snapshot());
  if (history.value.length > 30) history.value.shift();
  future.value = [];
  saved.value = false;
}
function add(type: string) {
  checkpoint();
  const m = modules[type],
    id = `${type}_${Date.now()}`;
  nodes.value.push({
    id,
    label: m.name,
    position: {
      x: 100 + (nodes.value.length % 3) * 230,
      y: 100 + Math.floor(nodes.value.length / 3) * 140,
    },
    data: {
      name: "",
      module: type,
      config: structuredClone(m.defaults),
      timeout_seconds: 600,
      retries: 0,
    },
  });
  if(type==="foreach"||type==="server_list"){
    const endType=type==="foreach"?"loop_end":"server_list_end";
    const endID=`${endType}_${Date.now()+1}`;nodes.value[nodes.value.length-1].data.config.end_node_id=endID;
    nodes.value.push({id:endID,label:modules[endType].name,position:{x:330+(nodes.value.length%3)*30,y:180+nodes.value.length*20},data:{name:"",module:endType,config:{start_node_id:id},timeout_seconds:600,retries:0}});
    edges.value.push({id:`e_${Date.now()}`,source:id,target:endID,condition:"success"});
  }
}
function connect(e: any) {
  checkpoint();
  const condition=["true","false"].includes(e.sourceHandle)?e.sourceHandle:"success";addEdges([{ ...e, condition, label:condition==="success"?"":condition, id: `e_${Date.now()}` }]);
}
function choose(e: any) {
  selectedEdge.value = undefined;
  edges.value = edges.value.map((edge) => ({ ...edge, selected: false }));
  selected.value = e.node;
}
function editNode(e:any){choose(e);nodeEditorOpen.value=true}
function chooseEdge(e: any) {
  nodeEditorOpen.value=false;
  selected.value = undefined;
  selectedEdge.value = e.edge;
  edges.value = edges.value.map((edge) => ({
    ...edge,
    selected: edge.id === e.edge.id,
  }));
}
function clearSelection() {
  nodeEditorOpen.value=false;
  selected.value = undefined;
  selectedEdge.value = undefined;
  edges.value = edges.value.map((edge) => ({ ...edge, selected: false }));
}
function restore(raw: string) {
  const x = JSON.parse(raw);
  nodes.value = x.nodes;
  edges.value = x.edges;
  selected.value = undefined;
  nodeEditorOpen.value=false;
}
function undo() {
  if (!history.value.length) return;
  future.value.push(snapshot());
  restore(history.value.pop()!);
  saved.value = false;
}
function redo() {
  if (!future.value.length) return;
  history.value.push(snapshot());
  restore(future.value.pop()!);
  saved.value = false;
}
function removeSelected() {
  if (!selected.value && !selectedEdge.value) return;
  checkpoint();
  if (selectedEdge.value) {
    edges.value = edges.value.filter((e) => e.id !== selectedEdge.value.id);
  } else {
    const nodeID = selected.value.id, removeIDs=new Set([nodeID]);
    if(["foreach","server_list"].includes(selected.value.data.module)&&selected.value.data.config.end_node_id)removeIDs.add(selected.value.data.config.end_node_id);
    if(["loop_end","server_list_end"].includes(selected.value.data.module)&&selected.value.data.config.start_node_id)removeIDs.add(selected.value.data.config.start_node_id);
    nodes.value = nodes.value.filter((n) => !removeIDs.has(n.id));
    edges.value = edges.value.filter(
      (e) => !removeIDs.has(e.source) && !removeIDs.has(e.target),
    );
  }
  clearSelection();
  nodeEditorOpen.value=false;
  saved.value = false;
}
function duplicate() {
  if (!selected.value) return;
  checkpoint();
  const n = structuredClone(selected.value),
    id = `${n.data.module}_${Date.now()}`;
  n.id = id;
  n.position = { x: n.position.x + 40, y: n.position.y + 40 };
  nodes.value.push(n);
  selected.value = n;
}
function beginCreate() {
  name.value = "新工作流";
  triggerType.value = "any";
  triggerGlob.value = "v*";
  enabled.value = true;
  createOpen.value = true;
}
function startEditor() {
  editing.value = 0;
  nodes.value = [];
  edges.value = [];
  history.value = [];
  future.value = [];
  createOpen.value = false;
  saved.value = false;
  savedSignature.value="";
}
function applyDefinition(definition:any,prefix="e"){
  nodes.value=(definition.nodes||[]).map((n:any)=>({id:n.id,label:n.name||modules[n.type]?.name||n.type,position:n.position||{x:100,y:100},data:{name:n.name||"",module:n.type,config:normalizeNodeConfig(n.type,n.config),timeout_seconds:n.timeout_seconds||600,retries:n.retries||0}}));
  edges.value=(definition.edges||[]).map((e:any,i:number)=>({id:`${prefix}${i}_${Date.now()}`,source:e.from,target:e.to,condition:e.condition||"success",label:e.condition&&e.condition!=="success"?e.condition:""}));
}
async function edit(id: number) {
  const w = await api<any>(`/api/projects/${pid}/workflows/${id}`);
  editing.value = id;
  name.value = w.name;
  enabled.value = w.enabled;
  triggerType.value = w.trigger_type;
  triggerGlob.value = w.trigger_glob;
  applyDefinition(w.definition,"e");
  history.value = [];
  future.value = [];
  baseRevisionID.value=0;
  await nextTick();savedSignature.value=persistedSignature.value;saved.value = true;
}
async function openWorkflowHistory(){if(!editing.value)return;workflowHistory.value=await api(`/api/projects/${pid}/workflows/${editing.value}/history`);historyPreview.value=undefined;historyOpen.value=true;if(workflowHistory.value.length)await viewWorkflowRevision(workflowHistory.value[0])}
async function viewWorkflowRevision(item:any){historyPreview.value=await api(`/api/projects/${pid}/workflows/${editing.value}/history/${item.id}`)}
function applyWorkflowRevision(){if(!historyPreview.value)return;checkpoint();name.value=historyPreview.value.name;enabled.value=historyPreview.value.enabled;triggerType.value=historyPreview.value.trigger_type;triggerGlob.value=historyPreview.value.trigger_glob;applyDefinition(historyPreview.value.definition,"revision");baseRevisionID.value=historyPreview.value.id;saved.value=false;historyOpen.value=false;nextTick(()=>fitView({padding:.2}))}
const historyPreviewNodes=computed(()=>(historyPreview.value?.definition?.nodes||[]).map((n:any)=>({id:n.id,position:n.position||{x:100,y:100},data:{label:n.name||modules[n.type]?.name||n.type},sourcePosition:Position.Right,targetPosition:Position.Left})));
const historyPreviewEdges=computed(()=>(historyPreview.value?.definition?.edges||[]).map((e:any,i:number)=>({id:`history-${i}`,source:e.from,target:e.to,label:e.condition&&e.condition!=="success"?e.condition:""})));
async function save() {
  error.value = "";
  if (!name.value.trim() || !nodes.value.length) {
    error.value = "请填写名称并至少添加一个模块";
    return false;
  }
  const definition = {
    nodes: nodes.value.map((n) => ({
      id: n.id,
      name: n.data.name || "",
      type: n.data.module,
      config: n.data.config,
      timeout_seconds: n.data.timeout_seconds,
      retries: n.data.retries,
      position: n.position,
    })),
    edges: edges.value.map((e) => ({ from: e.source, to: e.target, condition:e.condition||"success" })),
  };
  try {
    const w = await api<any>(
      editing.value
        ? `/api/projects/${pid}/workflows/${editing.value}`
        : `/api/projects/${pid}/workflows`,
      {
        method: editing.value ? "PUT" : "POST",
        body: JSON.stringify({
          name: name.value,
          enabled: enabled.value,
          trigger_type: triggerType.value,
          trigger_glob: triggerGlob.value,
          base_revision_id: baseRevisionID.value||undefined,
          definition,
        }),
      },
    );
    editing.value = w.id || editing.value;
    baseRevisionID.value=0;
    await nextTick();savedSignature.value=persistedSignature.value;saved.value = true;
    await load();
    return true;
  } catch (e: any) {
    error.value = e.message;
    return false;
  }
}
async function run() {
  const created = await api<any>(`/api/projects/${pid}/workflows/${editing.value}/runs`, {
    method: "POST",
    body: JSON.stringify({ release_id: releaseID.value }),
  });
  runOpen.value = false;
  runDetailID.value = created.id;
  runDetailOpen.value = true;
}
watch(persistedSignature,signature=>{if(savedSignature.value)saved.value=signature===savedSignature.value;else if(editing.value!==undefined)saved.value=false});
function inputFocus() {
  return ["INPUT", "TEXTAREA", "SELECT"].includes(
    document.activeElement?.tagName || "",
  );
}
function beforeUnload(event:BeforeUnloadEvent){if(!saved.value){event.preventDefault();event.returnValue=""}}
async function resolveLeave(action:"save"|"discard"|"cancel"){
  const resolve=leaveResolver;leaveResolver=undefined;leaveOpen.value=false;
  if(!resolve)return;
  if(action==="cancel"){resolve(false);return}
  if(action==="save"){
    const ok=await save();
    if(!ok){resolve(false);return}
  }
  resolve(true);
}
onBeforeRouteLeave(()=>{
  if(saved.value)return true;
  leaveOpen.value=true;
  return new Promise<boolean>(resolve=>{leaveResolver=resolve});
});
function key(e: KeyboardEvent) {
  if (!editing.value && editing.value !== 0) return;
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") {
    e.preventDefault();
    save();
    return;
  }
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "j") { e.preventDefault(); window.dispatchEvent(new Event("ai-toggle")); return; }
  if ((e.ctrlKey || e.metaKey) && e.shiftKey && e.key.toLowerCase() === "n") { e.preventDefault(); window.dispatchEvent(new Event("ai-new")); return; }
  if (inputFocus()) return;
  if (["Delete", "Backspace"].includes(e.key)) {
    e.preventDefault();
    removeSelected();
  } else if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "d") {
    e.preventDefault();
    duplicate();
  } else if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "z") {
    e.preventDefault();
    e.shiftKey ? redo() : undo();
  } else if (e.key.toLowerCase() === "f") fitView({ padding: 0.2 });
  else if ((e.ctrlKey || e.metaKey) && e.key === "Enter" && editing.value)
    runOpen.value = true;
}
onMounted(async () => {
	await load();
	if (route.query.new === "1" && canDevelop.value) beginCreate();
	else if (Number(route.query.workflow)) await edit(Number(route.query.workflow));
	else { await router.replace("/workflows"); return; }
	document.addEventListener("keydown", key, true);
	window.addEventListener("beforeunload",beforeUnload);
});
onUnmounted(() => {document.removeEventListener("keydown", key, true);window.removeEventListener("beforeunload",beforeUnload);if(leaveResolver)leaveResolver(false)});
const moduleInfo = computed(() =>
  selected.value ? modules[selected.value.data.module] : null,
);
function serverScopeFor(nodeID:string){
  for(const start of nodes.value.filter((n:any)=>n.data.module==="server_list")){
    const end=start.data.config.end_node_id, seen=new Set<string>(), queue=edges.value.filter((e:any)=>e.source===start.id).map((e:any)=>e.target);
    while(queue.length){const id=queue.shift();if(!id||id===end||seen.has(id))continue;seen.add(id);for(const e of edges.value.filter((x:any)=>x.source===id))queue.push(e.target)}
    if(seen.has(nodeID))return start;
  }
  return undefined;
}
const selectedServerScope = computed(()=>selected.value?serverScopeFor(selected.value.id):undefined);
const canvasDefinition = computed(() => ({
  nodes: nodes.value.map((n) => ({ id:n.id, name:n.data.name||"", type:n.data.module, config:n.data.config, timeout_seconds:n.data.timeout_seconds, retries:n.data.retries, position:n.position })),
  edges: edges.value.map((e) => ({ from:e.source, to:e.target, condition:e.condition||"success" })),
}));
function applyAICanvas(canvas:any){
  applyDefinition(canvas,"ai_e_");
  saved.value=false; nextTick(()=>fitView({padding:.2}));
}
</script>
<template>
  <div class="workflow-editor">
    <header class="editor-toolbar">
      <button class="icon-btn" title="返回工作流列表" @click="router.push('/workflows')">
        <ArrowLeft />
      </button>
      <button v-if="project" class="editor-project" type="button" title="打开项目详情" @click="router.push(`/projects/${pid}`)">
        <span class="editor-project-icon"><FolderKanban/></span>
        <span><b>{{project.name}}</b><small>{{project.type==='scheduled'?'定时项目':'产物项目'}} · {{project.role}} · #{{project.id}}</small></span>
      </button>
      <div class="editor-name">
        <input v-model="name" @input="saved = false" /><span>{{
          saved ? "已保存" : "有未保存更改"
        }}</span>
      </div>
      <label class="switch"
        ><input v-model="enabled" type="checkbox" />启用</label
      >
	  <button v-if="project?.type==='artifact'" class="trigger-editor-button" type="button" title="编辑上传触发条件" @click="openTriggerEditor"><GitBranch/><span><small>触发条件</small><b>{{triggerLabel}}</b></span></button>
	  <button v-if="editing" class="btn secondary history-button" type="button" title="查看保存历史" @click="openWorkflowHistory"><HistoryIcon/>历史</button>
	  <div v-if="project?.type==='artifact'" class="release-preview-control">
        <select v-model.number="previewReleaseID" @change="loadPreview">
          <option :value="0">选择预览版本</option>
          <option
            v-for="release in releases"
            :key="release.id"
            :value="release.id"
          >
            {{ release.version }}
          </option>
        </select>
        <button
          class="icon-btn"
          title="预览版本文件"
          :disabled="!previewReleaseID"
          @click="openPreview"
        >
          <Eye />
        </button>
      </div>
      <div class="toolbar-group">
        <button
          class="icon-btn"
          title="撤销 Ctrl+Z"
          :disabled="!history.length"
          @click="undo"
        >
          <Undo2 /></button
        ><button
          class="icon-btn"
          title="重做 Ctrl+Shift+Z"
          :disabled="!future.length"
          @click="redo"
        >
          <Redo2 /></button
        ><button
          class="icon-btn"
          title="适应画布 F"
          @click="fitView({ padding: 0.2 })"
        >
          <Maximize />
        </button>
      </div>
	  <button v-if="editing&&canDevelop" class="btn secondary" @click="runOpen = true">
        <Play />运行</button
	  ><button v-if="canDevelop" class="btn" @click="save"><Save />保存</button>
    </header>
    <p v-if="error" class="editor-error">{{ error }}</p>
    <div class="editor-body">
	  <aside v-if="canDevelop" class="module-palette">
        <span class="eyebrow">模块</span>
        <details v-for="group in moduleGroups" :key="group.category" open class="module-group">
          <summary>{{group.category}} <small>{{group.items.length}}</small></summary>
          <div class="module-grid">
            <button v-for="entry in group.items" :key="entry[0] as string" :title="(entry[1] as any).desc" @click="add(entry[0] as string)"><span class="module-icon"><component :is="(entry[1] as any).icon"/></span><span><b>{{(entry[1] as any).name}}</b><small>{{(entry[1] as any).desc}}</small></span></button>
          </div>
        </details>
      </aside>
      <div class="flow-canvas">
        <VueFlow
          v-model:nodes="nodes"
          v-model:edges="edges"
          fit-view-on-init
          :delete-key-code="null"
          @connect="connect"
          @node-click="choose"
          @node-double-click="editNode"
          @edge-click="chooseEdge"
          @pane-click="clearSelection"
          ><template #node-default="p"
            ><div class="flow-node" :class="{ selected: p.selected, 'server-scoped':!!serverScopeFor(p.id) }">
              <Handle
                id="input"
                type="target"
                :position="Position.Left"
                title="连接上一步"
              />
              <component :is="modules[p.data.module]?.icon" />
              <div>
                <b>{{ p.data.name || modules[p.data.module]?.name }}</b
                ><small v-if="p.data.module==='server_list'">{{p.data.name ? `${modules[p.data.module]?.name} · ` : ''}}{{p.data.config.connection_ids?.length||0}} 台 · {{p.data.config.mode==='sequential'?'串行':`并行 ${p.data.config.concurrency||4}`}}</small><small v-else>{{ p.data.name ? `${modules[p.data.module]?.name} · ${p.id}` : p.id }}</small>
              </div>
              <template v-if="['remote_file_exists','value_match'].includes(p.data.module)">
                <Handle id="true" type="source" :position="Position.Right" style="top:35%" title="条件为真"/><Handle id="false" type="source" :position="Position.Right" style="top:68%" title="条件为假"/>
              </template><Handle v-else id="output" type="source" :position="Position.Right" title="连接下一步"/></div></template
        ></VueFlow>
        <div class="canvas-hint">拖动画布 · 滚轮缩放 · 选中节点或连线后按 Delete 删除</div>
      </div>
    </div>
    <AIChatPanel :project-id="pid" :workflow-id="editing||undefined" :workflow-name="name" :trigger-type="triggerType" :trigger-glob="triggerGlob" :canvas="canvasDefinition" @checkpoint="checkpoint" @apply="applyAICanvas" />
  </div>
  <Drawer v-model="historyOpen" wide title="工作流保存历史"><div class="workflow-history-layout"><aside class="workflow-history-list"><button v-for="item in workflowHistory" :key="item.id" :class="{active:historyPreview?.id===item.id}" @click="viewWorkflowRevision(item)"><span><b>{{new Date(item.created_at).toLocaleString()}}</b><small>{{item.created_by||'未知用户'}} · {{item.node_count}} 个节点 · {{item.edge_count}} 条连接</small></span><em>#{{item.id}}</em></button><EmptyState v-if="!workflowHistory.length" title="暂无保存历史"/></aside><section v-if="historyPreview" class="workflow-history-preview"><header><div><h3>{{historyPreview.name}}</h3><p>{{new Date(historyPreview.created_at).toLocaleString()}} · {{historyPreview.created_by||'未知用户'}}</p></div><button v-if="canDevelop" class="btn" @click="applyWorkflowRevision">应用到当前画布</button></header><div class="history-meta"><span>{{historyPreview.enabled?'已启用':'已停用'}}</span><span>{{historyPreview.trigger_type==='branch_glob'?`分支 ${historyPreview.trigger_glob}`:historyPreview.trigger_type==='tag_glob'?`Tag ${historyPreview.trigger_glob}`:historyPreview.trigger_type}}</span></div><div class="workflow-history-canvas"><VueFlow :nodes="historyPreviewNodes" :edges="historyPreviewEdges" fit-view-on-init :nodes-draggable="false" :nodes-connectable="false" :elements-selectable="false"><template #node-default="previewNode"><div class="history-preview-node"><component :is="modules[historyPreview.definition.nodes.find((item:any)=>item.id===previewNode.id)?.type]?.icon"/><span><b>{{previewNode.data.label}}</b><small>{{previewNode.id}}</small></span></div></template></VueFlow></div><details class="code-disclosure"><summary>查看历史 JSON</summary><pre>{{JSON.stringify(historyPreview.definition,null,2)}}</pre></details></section></div></Drawer>
  <Drawer v-model="previewOpen" title="版本文件预览">
    <div class="release-preview-summary">
      <b>{{
        releases.find((item) => item.id === previewReleaseID)?.version
      }}</b>
      <span>{{ previewFiles.length }} 个文件</span>
    </div>
    <div class="preview-file-list">
      <FileTree :files="previewFiles" />
      <EmptyState v-if="!previewFiles.length" title="该版本没有文件" />
    </div>
  </Drawer>
  <Drawer
    v-model="nodeEditorOpen"
  :title="selected?.data.name || moduleInfo?.name || '节点配置'"
  ><template v-if="selected"
    ><p class="muted">{{ moduleInfo?.desc }}</p>
      <label>节点名称<input v-model.trim="selected.data.name" maxlength="80" :placeholder="moduleInfo?.name || '输入便于识别的名称'" /></label>
      <label>节点 ID<input v-model="selected.id" /></label>
      <div v-if="supportsPattern" class="pattern-picker">
        <label>
          预览版本
          <select v-model.number="previewReleaseID" @change="loadPreview">
            <option :value="0">选择一个发布版本</option>
            <option
              v-for="release in releases"
              :key="release.id"
              :value="release.id"
            >
              {{ release.version }}
            </option>
          </select>
        </label>
        <label>
          文件正则表达式
          <input
            v-model="selected.data.config.file_pattern"
            placeholder="例如：^dist/.*\\.(js|css)$"
          />
        </label>
        <p v-if="!previewReleaseID" class="pattern-tip">
          选择一个版本后，可实时预览正则表达式匹配的文件。
        </p>
        <p v-else-if="patternResult.error" class="error">
          正则表达式无效：{{ patternResult.error }}
        </p>
        <div v-else class="pattern-results">
          <div class="pattern-result-head">
            <span>实时匹配</span><b>{{ patternResult.files.length }} 个文件</b>
          </div>
          <FileTree
            :files="patternResult.files.slice(0, 100)"
            action="select"
            @select="selectExactFile"
          />
          <small v-if="patternResult.files.length > 20">
            另有 {{ patternResult.files.length - 20 }} 个匹配文件
          </small>
        </div>
      </div>
      <template v-if="selected.data.module === 'archive'"
        ><label>输出路径<input v-model="selected.data.config.output" /></label
        ><label
          >格式<select v-model="selected.data.config.format">
            <option>zip</option>
            <option>tar.gz</option>
          </select></label
        ></template
      ><template v-else-if="selected.data.module === 'extract'"
        ><label
          >输出目录<input
            v-model="selected.data.config.output" /></label></template
      ><template
        v-else-if="
          ['sftp_upload', 'sftp_extract', 'ssh_command','remote_file_exists'].includes(
            selected.data.module,
          )
        "
        ><div v-if="selectedServerScope" class="notice compact-notice">服务器连接继承自“{{selectedServerScope.label}}”，可使用 <code v-pre>{{server.name}}</code> 等变量。</div><label v-else
          >服务器连接<select
            v-model.number="selected.data.config.connection_id"
          >
            <option :value="0">选择连接</option>
            <option v-for="c in connections" :value="c.id">
              {{ c.name }} · {{ c.host }}
            </option>
          </select></label
        ><template v-if="selected.data.module === 'remote_file_exists'">
          <label>远端文件路径<input v-model="selected.data.config.path" placeholder="/opt/app/current"/></label>
        </template><template v-else-if="selected.data.module === 'sftp_upload'"
          ><label
            >远端路径<input
              v-model="selected.data.config.destination" /></label></template
        ><template v-else-if="selected.data.module === 'sftp_extract'">
          <div class="notice compact-notice">
            自动使用当前发布中由 Action 上传的唯一 ZIP 或 tar.gz 压缩包。文件先在远端暂存目录解压，权限仅应用于本次解压内容。
          </div>
          <label
            >解压目标目录<input
              v-model="selected.data.config.destination"
              placeholder="/opt/application"
          /></label>
          <label
            >解压后权限<input
              v-model="selected.data.config.permission"
              inputmode="numeric"
              pattern="[0-7]{3,4}"
              placeholder="0755"
          /></label>
          <label class="checkbox-row"
            ><input
              v-model="selected.data.config.keep_archive"
              type="checkbox"
            />保留服务器上的压缩包</label
          > </template
        ><template v-else
          ><label
            >命令（每行一条）<textarea
              :value="(selected.data.config.commands || []).join('\n')"
              @input="
                selected.data.config.commands = (
                  $event.target as HTMLTextAreaElement
                ).value.split('\n')
              "
              rows="8"
            ></textarea></label
          ><label
            >远端工作目录<input
              v-model="
                selected.data.config.work_dir
              " /></label></template></template
      ><template v-else-if="selected.data.module === 'value_match'">
        <label>待判断内容<textarea v-model="selected.data.config.actual" rows="4" placeholder="{{steps.command.outputs.stdout}}"></textarea></label>
        <label>比较方式<select v-model="selected.data.config.operator"><option value="exists">存在</option><option value="equals">等于</option><option value="not_equals">不等于</option><option value="contains">包含</option><option value="regex">正则匹配</option></select></label>
        <label v-if="selected.data.config.operator!=='exists'">期望值<input v-model="selected.data.config.expected"/></label><label class="checkbox-row"><input v-model="selected.data.config.ignore_case" type="checkbox">忽略大小写</label>
      </template><template v-else-if="selected.data.module === 'string_split'">
        <label>输入字符串<textarea v-model="selected.data.config.value" rows="4" placeholder="{{steps.command.outputs.stdout}}"></textarea></label><label>分隔符<input v-model="selected.data.config.separator"/></label><label class="checkbox-row"><input v-model="selected.data.config.regex" type="checkbox">分隔符使用正则表达式</label><label class="checkbox-row"><input v-model="selected.data.config.trim" type="checkbox">移除首尾空格</label><label class="checkbox-row"><input v-model="selected.data.config.drop_empty" type="checkbox">忽略空项</label>
      </template><template v-else-if="selected.data.module === 'foreach'">
        <label>列表输出引用<input v-model="selected.data.config.items_from" placeholder="steps.split.outputs.items"/></label><label>执行模式<select v-model="selected.data.config.mode"><option value="parallel">并行</option><option value="sequential">串行</option></select></label><label>循环并发上限<input v-model.number="selected.data.config.concurrency" type="number" min="1" max="100"/></label><label>循环结束节点<input v-model="selected.data.config.end_node_id" readonly/></label>
      </template><template v-else-if="selected.data.module === 'loop_end'">
        <div class="notice compact-notice">该节点由列表循环自动管理，并汇总每次循环输出。</div>
      </template><template v-else-if="selected.data.module === 'server_list'">
        <label>选择服务器</label><div class="server-choice-list"><label v-for="c in connections" :key="c.id" class="server-choice"><input v-model="selected.data.config.connection_ids" type="checkbox" :value="c.id"/><span><b>{{c.name}}</b><small>{{c.username}}@{{c.host}}:{{c.port}}</small></span></label><p v-if="!connections.length" class="muted">暂无服务器连接，请先在 SSH 资源中添加。</p></div>
        <label>执行模式<select v-model="selected.data.config.mode"><option value="parallel">并行执行</option><option value="sequential">串行执行</option></select></label><label v-if="selected.data.config.mode==='parallel'">并发上限<input v-model.number="selected.data.config.concurrency" type="number" min="1" :max="Math.max(1,selected.data.config.connection_ids.length)"/></label><label>服务器列表结束节点<input v-model="selected.data.config.end_node_id" readonly/></label>
        <div class="notice compact-notice">范围内所有节点会为每台服务器创建独立任务；服务器模块将强制使用当前服务器。</div>
      </template><template v-else-if="selected.data.module === 'server_list_end'">
        <div class="notice compact-notice">该节点由服务器列表自动管理，并按选择顺序汇总各服务器状态和输出。</div>
      </template><template v-else-if="selected.data.module === 'http_webhook'"
        ><label
          >请求方法<select v-model="selected.data.config.method">
            <option>POST</option>
            <option>PUT</option>
            <option>PATCH</option>
          </select></label
        ><label>URL<input v-model="selected.data.config.url" /></label
        ><label
          >请求正文<textarea
            v-model="selected.data.config.body"
            rows="7"
          ></textarea></label></template
      ><template v-else
        ><label
          >算法<select v-model="selected.data.config.algorithm">
            <option>sha256</option>
            <option>sha512</option>
          </select></label
        ><label>期望摘要<input v-model="selected.data.config.expected" /></label
      ></template>
      <label v-if="['ssh_command','http_webhook'].includes(selected.data.module)" class="checkbox-row"><input v-model="selected.data.config.sensitive_output" type="checkbox">输出包含敏感信息（加密保存且不展示）</label>
      <details class="code-disclosure" v-pre><summary>可用模板变量</summary><code>{{env.NAME}}</code><br><code>{{env.global.NAME}}</code><br><code>{{env.project.NAME}}</code><br><code>{{steps.node_id.outputs.stdout}}</code><br><code>{{loop.item}}</code><br><code>{{server.id}}</code> · <code>{{server.name}}</code> · <code>{{server.host}}</code> · <code>{{server.username}}</code></details>
      <div class="form-grid">
        <label
          >超时（秒）<input
            v-model.number="selected.data.timeout_seconds"
            type="number"
            min="1" /></label
        ><label
          >重试次数<input
            v-model.number="selected.data.retries"
            type="number"
            min="0"
            max="10"
        /></label>
      </div>
      <div class="panel-actions">
        <button v-if="!['foreach','loop_end','server_list','server_list_end'].includes(selected.data.module)" class="btn secondary" @click="duplicate"><Copy />复制</button
        ><button class="btn danger-ghost" @click="removeSelected">
          <Trash2 />删除
        </button>
      </div>
      <details class="code-disclosure">
        <summary>高级 · 查看 JSON</summary>
        <pre>{{ JSON.stringify(selected.data.config, null, 2) }}</pre>
      </details></template
    ></Drawer
  ><Modal
    v-model="createOpen"
    title="新建工作流"
    description="先设置触发条件，随后进入可视化编辑器。"
    ><label>名称<input v-model="name" autofocus /></label
    ><label v-if="project?.type==='artifact'"
      >触发条件<select v-model="triggerType">
        <option value="any">任意版本</option>
        <option value="tag">仅 Tag</option>
        <option value="commit">仅 Commit</option>
        <option value="tag_glob">Tag Glob</option>
        <option value="branch_glob">分支匹配</option>
      </select></label
    ><label v-if="project?.type==='artifact'&&['tag_glob','branch_glob'].includes(triggerType)"
      >{{triggerType==='branch_glob'?'分支匹配规则':'Tag 匹配规则'}}<input v-model="triggerGlob" :placeholder="triggerType==='branch_glob'?'main 或 feature/*':'v*'"
    /></label>
    <div class="panel-actions">
      <button class="btn secondary" @click="router.push('/workflows')">取消</button
      ><button class="btn" @click="startEditor">进入编辑器</button>
    </div></Modal
  ><Modal v-model="triggerOpen" title="编辑触发条件" description="保存工作流后，新上传的产物会按此条件自动触发。"><label>触发条件<select v-model="triggerDraft.type"><option value="any">任意版本</option><option value="tag">仅 Tag</option><option value="commit">所有分支 Commit</option><option value="tag_glob">Tag 匹配</option><option value="branch_glob">分支匹配</option></select></label><label v-if="['tag_glob','branch_glob'].includes(triggerDraft.type)">{{triggerDraft.type==='branch_glob'?'分支 glob':'Tag glob'}}<input v-model="triggerDraft.glob" :placeholder="triggerDraft.type==='branch_glob'?'main 或 feature/*':'v* 或 release-*'"></label><div v-if="triggerDraft.type==='branch_glob'" class="notice notice-warning">仅匹配带分支标识的 Commit 产物；Tag 产物不会触发。</div><div class="panel-actions"><button class="btn secondary" @click="triggerOpen=false">取消</button><button class="btn" :disabled="['tag_glob','branch_glob'].includes(triggerDraft.type)&&!triggerDraft.glob.trim()" @click="applyTrigger">应用条件</button></div></Modal
  ><Modal :model-value="leaveOpen" title="存在未保存的更改" description="离开后，当前画布上的更改将丢失。" locked><div class="panel-actions leave-actions"><button class="btn secondary" @click="resolveLeave('cancel')">取消</button><button class="btn danger-ghost" @click="resolveLeave('discard')">放弃更改</button><button class="btn" @click="resolveLeave('save')"><Save/>保存并离开</button></div></Modal
  ><Modal
    v-model="runOpen"
    title="手动运行"
    description="选择一个已有发布作为本次运行输入。"
    ><label v-if="project?.type==='artifact'"
      >发布版本<select v-model.number="releaseID">
		<option :value="0" :disabled="project?.type==='artifact'">{{project?.type==='scheduled'?'空产物上下文':'选择版本'}}</option>
        <option v-for="r in releases" :value="r.id">{{ r.version }}</option>
      </select></label
    >
    <div class="panel-actions">
      <button class="btn secondary" @click="runOpen = false">取消</button
      ><button class="btn" :disabled="project?.type==='artifact'&&!releaseID" @click="run">
        <Play />开始运行
      </button>
    </div></Modal>
  <RunDetailDrawer v-model="runDetailOpen" :project-id="pid" :run-id="runDetailID" />
</template>
<style scoped>
.editor-project { max-width: 210px; min-width: 150px; height: 48px; padding: 0 14px 0 8px; border: 0; border-right: 1px solid var(--border); background: transparent; display: flex; align-items: center; gap: 9px; text-align: left; cursor: pointer; }
.editor-project:hover { background: var(--surface-2); border-radius: 11px; }
.editor-project-icon { width: 32px; height: 32px; flex: 0 0 auto; border-radius: 10px; background: var(--surface-2); display: grid; place-items: center; }
.editor-project-icon svg { width: 16px; }
.editor-project > span:last-child { min-width: 0; display: grid; gap: 3px; }
.editor-project b, .editor-project small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.editor-project b { font-size: 13px; }
.editor-project small { font-size: 9px; color: var(--muted); }
.trigger-editor-button { height: 42px; padding: 0 10px; border: 1px solid var(--border); border-radius: 11px; background: #fff; display: flex; align-items: center; gap: 7px; text-align: left; cursor: pointer; }
.trigger-editor-button:hover { border-color: #aaa; background: var(--surface-2); }
.trigger-editor-button > svg { width: 16px; }
.trigger-editor-button > span { display: grid; gap: 1px; }
.trigger-editor-button small { font-size: 8px; }
.trigger-editor-button b { max-width: 120px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 10px; }
.history-button { white-space: nowrap; }
.workflow-history-layout { display: grid; grid-template-columns: 280px minmax(0,1fr); gap: 18px; min-height: 620px; }
.workflow-history-list { display: grid; align-content: start; gap: 7px; padding-right: 14px; border-right: 1px solid var(--border); }
.workflow-history-list > button { width: 100%; padding: 12px; border: 1px solid var(--border); border-radius: 12px; background: #fff; display: flex; gap: 8px; text-align: left; cursor: pointer; }
.workflow-history-list > button:hover, .workflow-history-list > button.active { border-color: #888; background: var(--surface-2); }
.workflow-history-list button span { min-width: 0; display: grid; gap: 4px; flex: 1; }
.workflow-history-list button b { font-size: 12px; }
.workflow-history-list button small { font-size: 9px; }
.workflow-history-list button em { color: var(--muted); font-size: 10px; font-style: normal; }
.workflow-history-preview { min-width: 0; }
.workflow-history-preview > header { display: flex; align-items: center; justify-content: space-between; gap: 14px; margin-bottom: 12px; }
.workflow-history-preview h3, .workflow-history-preview p { margin: 0; }
.workflow-history-preview p { margin-top: 4px; color: var(--muted); font-size: 11px; }
.history-meta { display: flex; gap: 7px; margin-bottom: 12px; }
.history-meta span { padding: 4px 8px; border-radius: 999px; background: var(--surface-2); font-size: 10px; }
.workflow-history-canvas { height: 430px; border: 1px solid var(--border); border-radius: 15px; overflow: hidden; background: var(--surface-2); }
.history-preview-node { min-width: 150px; padding: 10px; border: 1px solid #ccc; border-radius: 12px; background: #fff; display: flex; align-items: center; gap: 8px; }
.history-preview-node > svg { width: 17px; }
.history-preview-node span { display: grid; gap: 2px; }
.history-preview-node small { font-size: 8px; }
.leave-actions { flex-wrap: wrap; }
@media (max-width: 1100px) {
  .editor-project { min-width: 42px; width: 42px; padding: 0 5px; border-right: 0; }
  .editor-project > span:last-child { display: none; }
  .trigger-editor-button > span { display: none; }
}
@media (max-width: 760px) {
  .workflow-history-layout { grid-template-columns: 1fr; }
  .workflow-history-list { max-height: 190px; overflow: auto; border-right: 0; border-bottom: 1px solid var(--border); padding: 0 0 12px; }
  .workflow-history-canvas { height: 330px; }
  .history-button { width: 38px; padding: 0; font-size: 0; }
}
</style>
