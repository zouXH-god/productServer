<script setup lang="ts">
// @ts-nocheck
import { computed, nextTick, onUnmounted, ref, watch } from "vue";
import { CheckCircle2, XCircle, Clock3, LoaderCircle, Ban, Terminal, GitCommit, GitBranch, Box, ServerCog } from "lucide-vue-next";
import { VueFlow, Position } from "@vue-flow/core";
import "@vue-flow/core/dist/style.css";
import { api } from "../api";
import Drawer from "./Drawer.vue";
import StatusBadge from "./StatusBadge.vue";

const props = defineProps<{ modelValue: boolean; projectId?: number; runId?: number }>();
const emit = defineEmits<{ "update:modelValue": [value: boolean] }>();
const visible = computed({ get: () => props.modelValue, set: value => emit("update:modelValue", value) });
const detail = ref<any>(), selected = ref<any>(), logs = ref<any[]>([]);
const logViewport = ref<HTMLElement>(), followLogs = ref(true);
let timer: any;
const names: any = { archive:"归档压缩",extract:"解压文件",sftp_upload:"SFTP 上传",sftp_extract:"上传并解压",checksum_verify:"摘要校验",foreach:"列表循环",loop_end:"循环结束",server_list:"服务器列表",server_list_end:"服务器列表结束",remote_file_exists:"远端文件存在",value_match:"值匹配",string_split:"字符串分割",ssh_command:"SSH 命令",http_webhook:"HTTP 回调" };
function duration(start: any, end: any) { if (!start) return "—"; const ms=Math.max(0,new Date(end||Date.now()).getTime()-new Date(start).getTime()); if(ms<1000)return`${ms}ms`;const s=Math.floor(ms/1000);return s<60?`${s}s`:`${Math.floor(s/60)}m ${s%60}s`; }
function statusIcon(status: string) { return status==="succeeded"?CheckCircle2:status==="failed"?XCircle:status==="running"?LoaderCircle:status==="cancelled"?Ban:Clock3; }
async function loadLogs() { if (detail.value) logs.value=await api(`/api/projects/${detail.value.project_id}/runs/${detail.value.id}/logs${selected.value?`?node=${encodeURIComponent(selected.value.node_key)}`:""}`); }
function scrollLogsToBottom(){nextTick(()=>{if(followLogs.value&&logViewport.value)logViewport.value.scrollTop=logViewport.value.scrollHeight})}
function handleLogScroll(){const el=logViewport.value;if(!el)return;followLogs.value=el.scrollHeight-el.scrollTop-el.clientHeight<=24}
async function refresh() {
  if (!props.projectId || !props.runId) return;
  detail.value=await api(`/api/projects/${props.projectId}/runs/${props.runId}`);
  if(!selected.value&&detail.value.nodes?.length)selected.value=detail.value.nodes.find((node:any)=>node.status==="running")||detail.value.nodes[0];else if(selected.value){selected.value=detail.value.nodes.find((node:any)=>node.id===selected.value.id)||selected.value;const running=detail.value.nodes.find((node:any)=>node.status==="running");if(selected.value.status==="pending"&&running)selected.value=running;}
  await loadLogs();
  if(!["queued","running"].includes(detail.value.status))clearInterval(timer);
}
function selectNode(node:any){selected.value=node;followLogs.value=true;loadLogs().then(scrollLogsToBottom);}
const graphNodes=computed(()=>{if(!detail.value)return[];const states=new Map((detail.value.nodes||[]).filter((x:any)=>x.iteration_index==null&&!x.server_connection_id).map((x:any)=>[x.node_key,x]));for(const n of detail.value.nodes||[]){if(!n.server_connection_id)continue;const key=n.node_key.replace(/\[server:\d+\]$/,""),old:any=states.get(key),rank:any={failed:5,running:4,cancelled:3,pending:2,succeeded:1,skipped:0};if(!old||rank[n.status]>rank[old.status])states.set(key,n)}return(detail.value.definition?.nodes||[]).map((n:any)=>({id:n.id,position:n.position||{x:100,y:100},data:{label:n.name||names[n.type]||n.type,state:states.get(n.id)},class:`run-node-${states.get(n.id)?.status||"pending"}`,sourcePosition:Position.Right,targetPosition:Position.Left}))});
const graphEdges=computed(()=>(detail.value?.definition?.edges||[]).map((edge:any,index:number)=>({id:`r${index}`,source:edge.from,target:edge.to,label:edge.condition&&edge.condition!=="success"?edge.condition:"",animated:detail.value?.status==="running"})));
const serverGroups=computed(()=>{const groups=new Map<number,any>();for(const node of detail.value?.nodes||[]){if(!node.server_connection_id)continue;if(!groups.has(node.server_connection_id))groups.set(node.server_connection_id,{id:node.server_connection_id,index:node.server_index,server:node.server,nodes:[]});groups.get(node.server_connection_id).nodes.push(node)}return[...groups.values()].sort((a,b)=>(a.index??0)-(b.index??0))});
function nodeName(node:any){const key=String(node.node_key||"").replace(/\[server:\d+\]$/," ").replace(/\[\d+\]$/," ").trim();const definitionNode=detail.value?.definition?.nodes?.find((item:any)=>item.id===key);return definitionNode?.name||names[node.module]||node.module}
watch(() => [props.modelValue, props.projectId, props.runId], async ([open]) => {
  clearInterval(timer);
  if (!open) { detail.value=undefined;selected.value=undefined;logs.value=[];return; }
  selected.value=undefined;logs.value=[];followLogs.value=true;await refresh();scrollLogsToBottom();
  if (["queued","running"].includes(detail.value?.status)) timer=setInterval(refresh,750);
}, { immediate:true });
watch(logs,scrollLogsToBottom,{deep:false});
onUnmounted(()=>clearInterval(timer));
</script>

<template>
  <Drawer v-model="visible" wide :title="detail?`${detail.workflow_name} · 运行 #${detail.id}`:'运行详情'">
    <div v-if="detail" class="run-detail">
      <section class="run-summary card"><div><small>状态</small><StatusBadge :status="detail.status"/></div><div><small>{{detail.trigger_source==='schedule'?'计划时间':'版本'}}</small><b><Box/>{{detail.trigger_source==='schedule'?new Date(detail.scheduled_for).toLocaleString():detail.version}}</b></div><div><small>分支</small><b><GitBranch/>{{detail.branch||'—'}}</b></div><div><small>触发来源</small><b class="mono"><GitCommit/>{{({artifact:'产物上传',manual:'手动运行',schedule:'定时计划'} as any)[detail.trigger_source]||detail.trigger_source||'产物上传'}}</b></div><div><small>总耗时</small><b>{{duration(detail.started_at,detail.finished_at)}}</b></div></section>
      <p v-if="detail.error_summary" class="notice notice-danger">{{detail.error_summary}}</p>
      <div class="run-visual"><aside class="run-jobs"><b>所有节点</b><template v-if="serverGroups.length"><div v-for="group in serverGroups" :key="group.id" class="run-server-group"><strong><ServerCog/>{{group.server?.name||`服务器 #${group.id}`}}<small>{{group.server?`${group.server.username}@${group.server.host}:${group.server.port}`:''}}</small></strong><button v-for="node in group.nodes" :key="node.id" :class="{active:selected?.id===node.id}" @click="selectNode(node)"><component :is="statusIcon(node.status)" :class="{spin:node.status==='running'}"/><span>{{nodeName(node)}}<small>{{node.node_key}} · {{duration(node.started_at,node.finished_at)}}</small></span></button></div></template><button v-for="node in (detail.nodes||[]).filter((x:any)=>!x.server_connection_id)" :key="node.id" :class="{active:selected?.id===node.id}" @click="selectNode(node)"><component :is="statusIcon(node.status)" :class="{spin:node.status==='running'}"/><span>{{nodeName(node)}}<small>{{node.node_key}} · {{duration(node.started_at,node.finished_at)}}</small></span></button></aside><div class="run-graph"><VueFlow :id="`run-detail-${detail.id}`" :nodes="graphNodes" :edges="graphEdges" fit-view-on-init :nodes-draggable="false" :nodes-connectable="false" :elements-selectable="false"><template #node-default="node"><div class="run-graph-node"><component :is="statusIcon(node.data.state?.status||'pending')"/><span><b>{{node.data.label}}</b><small>{{node.id}}</small></span><em>{{duration(node.data.state?.started_at,node.data.state?.finished_at)}}</em></div></template></VueFlow></div></div>
      <section v-if="selected" class="run-output-grid"><article class="card run-output"><header><Terminal/>节点日志 <span>{{selected.node_key}} · {{followLogs?'自动跟随':'已暂停跟随'}}</span></header><pre ref="logViewport" @scroll="handleLogScroll"><template v-for="line in logs">[{{line.time}}] [{{line.stream}}] {{line.message}}{{'\n'}}</template><span v-if="!logs.length" class="muted">暂无日志记录</span></pre></article><article class="card run-node-meta"><header>节点输出</header><div class="detail-list"><div><span>状态</span><StatusBadge :status="selected.status"/></div><div><span>尝试次数</span><b>{{selected.attempts}}</b></div><div><span>耗时</span><b>{{duration(selected.started_at,selected.finished_at)}}</b></div></div><p v-if="selected.error_summary" class="error">{{selected.error_summary}}</p><div v-if="selected.outputs_sensitive" class="notice notice-warning">输出包含敏感信息，已加密保存。</div><pre v-else-if="selected.outputs">{{JSON.stringify(selected.outputs,null,2)}}</pre><p v-else class="muted">该节点没有结构化输出。</p></article></section>
    </div>
  </Drawer>
</template>
