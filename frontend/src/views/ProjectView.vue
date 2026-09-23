<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  Workflow,
  KeyRound,
  Package,
  Trash2,
  Copy,
  ChevronDown,
  Users,
  Plus,
  ArrowUpRight,
  Play,
  Pencil,
  History,
  GitBranch,
  Download,
  HardDrive,
  Settings,
} from "lucide-vue-next";
import { api, authDownload } from "../api";
import Modal from "../components/Modal.vue";
import Drawer from "../components/Drawer.vue";
import EmptyState from "../components/EmptyState.vue";
import FileTree from "../components/FileTree.vue";
import EnvironmentVariables from "../components/EnvironmentVariables.vue";
import RunDetailDrawer from "../components/RunDetailDrawer.vue";
import StatusBadge from "../components/StatusBadge.vue";
import { toast } from "../toast";
const route = useRoute(),
  router = useRouter(),
  id = Number(route.params.id),
  project = ref<any>(),
  token = ref<any>(),
  releases = ref<any[]>([]),
  workflows = ref<any[]>([]),
  runs = ref<any[]>([]),
  targetProjects = ref<any[]>([]),
  selected = ref<any>(),
  accessRecords = ref<any[]>([]),
  rotating = ref(false),
  newToken = ref(""),
  deleting = ref(false),
  copied = ref("");
const members=ref<any[]>([]),memberOpen=ref(false),memberForm=ref({username:"",role:"viewer"});
const runTarget=ref<any>(),historyTarget=ref<any>(),deleteWorkflowTarget=ref<any>(),releaseID=ref(0),running=ref(false);
const runDetailOpen=ref(false),runDetailProjectID=ref<number>(),runDetailID=ref<number>();
const copyTarget=ref<any>(),copyForm=ref({target_project_id:id,name:""});
const capacityOpen=ref(false),capacityValue=ref<number|null>(null),capacityUnit=ref(1024**3),capacitySaving=ref(false);
function formatBytes(value:number){if(!value)return '0 B';const units=['B','KB','MB','GB','TB'];const index=Math.min(Math.floor(Math.log(value)/Math.log(1024)),units.length-1);return `${(value/1024**index).toFixed(index===0?0:1)} ${units[index]}`}
function openCapacity(){const bytes=Number(project.value?.max_artifact_bytes||0);capacityUnit.value=bytes>=1024**4?1024**4:bytes>=1024**3?1024**3:1024**2;capacityValue.value=bytes?Number((bytes/capacityUnit.value).toFixed(2)):null;capacityOpen.value=true}
async function saveCapacity(){capacitySaving.value=true;try{await api(`/api/projects/${id}`,{method:'PUT',body:JSON.stringify({max_artifact_bytes:capacityValue.value?Math.round(capacityValue.value*capacityUnit.value):0})});capacityOpen.value=false;await load()}finally{capacitySaving.value=false}}
async function load() {
  project.value=await api<any>(`/api/projects/${id}`);[workflows.value,runs.value,members.value,targetProjects.value]=await Promise.all([api<any[]>(`/api/projects/${id}/workflows`),api<any[]>(`/api/projects/${id}/runs`),api<any[]>(`/api/projects/${id}/members`),api<any[]>(`/api/projects`)]);if(project.value.type==='artifact'){releases.value=await api<any[]>(`/api/projects/${id}/releases`);token.value=await api<any>(`/api/projects/${id}/token`).catch(()=>null)}else{releases.value=[];token.value=null}
}
const canAdmin=computed(()=>['owner','admin'].includes(project.value?.role)),canDevelop=computed(()=>['owner','admin','developer'].includes(project.value?.role));
function workflowTriggerLabel(flow:any){if(flow.trigger_type==='tag_glob')return `Tag ${flow.trigger_glob||'v*'}`;if(flow.trigger_type==='branch_glob')return `分支 ${flow.trigger_glob||'main'}`;return ({any:'任意版本',tag:'仅 Tag',commit:'所有分支 Commit'} as any)[flow.trigger_type]||flow.trigger_type}
async function addMember(){await api(`/api/projects/${id}/members`,{method:'POST',body:JSON.stringify(memberForm.value)});memberOpen.value=false;memberForm.value={username:'',role:'viewer'};await load()}
async function setRole(member:any,role:string){await api(`/api/projects/${id}/members/${member.user_id}`,{method:'PUT',body:JSON.stringify({role})});await load()}
async function removeMember(member:any){await api(`/api/projects/${id}/members/${member.user_id}`,{method:'DELETE'});await load()}
async function detail(r: any) {
  const [release, accesses] = await Promise.all([
    api(`/api/projects/${id}/releases/${r.id}`),
    api<any[]>(`/api/projects/${id}/releases/${r.id}/accesses`),
  ]);
  selected.value = release;
  accessRecords.value = Array.isArray(accesses) ? accesses : [];
}
function downloadArtifact(file: any) {
  authDownload(
    `/api/projects/${id}/releases/${selected.value.id}/files/${file.id}/download`,
    file.name.split("/").pop() || file.name,
  );
}
async function rotate() {
  const r = await api<any>(`/api/projects/${id}/token/rotate`, {
    method: "POST",
  });
  newToken.value = r.token;
  rotating.value = false;
  await load();
}
async function remove() {
  await api(`/api/projects/${id}`, { method: "DELETE" });
  router.push("/projects");
}
function editWorkflow(flow:any){router.push({path:`/workflows/editor/${id}`,query:{workflow:String(flow.id)}})}
function prepareCopyWorkflow(flow:any){copyTarget.value=flow;copyForm.value={target_project_id:id,name:`${flow.name} 副本`}}
async function copyWorkflow(){if(!copyTarget.value)return;running.value=true;try{await api(`/api/projects/${id}/workflows/${copyTarget.value.id}/copy`,{method:'POST',body:JSON.stringify(copyForm.value)});copyTarget.value=undefined;await load()}finally{running.value=false}}
function prepareRun(flow:any){runTarget.value=flow;releaseID.value=project.value.type==='artifact'?(releases.value[0]?.id||0):0}
async function runWorkflow(){if(!runTarget.value)return;running.value=true;try{const created=await api<any>(`/api/projects/${id}/workflows/${runTarget.value.id}/runs`,{method:'POST',body:JSON.stringify({release_id:releaseID.value})});runTarget.value=undefined;await router.push({path:'/runs',query:{project:String(id),workflow:String(created.workflow_id||''),run:String(created.id)}})}finally{running.value=false}}
function openHistory(flow:any){historyTarget.value={flow,runs:runs.value.filter((item:any)=>item.workflow_id===flow.id)}}
function viewRun(item:any){historyTarget.value=undefined;runDetailProjectID.value=id;runDetailID.value=item.id;runDetailOpen.value=true}
async function deleteWorkflow(){if(!deleteWorkflowTarget.value)return;await api(`/api/projects/${id}/workflows/${deleteWorkflowTarget.value.id}`,{method:'DELETE'});deleteWorkflowTarget.value=undefined;await load()}
async function copy(text: string, key = "x") {
  await navigator.clipboard.writeText(text);
  copied.value = key;
  toast(key === "token" ? "Token 复制成功" : "复制成功");
  setTimeout(() => (copied.value = ""), 1400);
}
const actionExample = computed(
  () =>
    `- uses: shiran/product-server-action@v1\n  with:\n    url: https://your-server.example.com\n    token: \${{ secrets.ARTIFACT_SERVER_TOKEN }}\n    path: dist/**`,
);
onMounted(load);
</script>
<template>
  <template v-if="project"
    ><div class="page-heading">
      <div>
        <router-link class="back-link" to="/projects">← 所有项目</router-link>
        <h1>{{ project.name }}</h1>
        <p>{{project.type==='scheduled'?'定时项目':'产物项目'}} · {{project.role}} · 创建于 {{ new Date(project.created_at).toLocaleString() }}</p>
      </div>
      <div class="actions">
        <router-link class="btn" to="/workflows"
          ><Workflow />工作流</router-link
        ><button v-if="project.role==='owner'" class="btn danger-ghost" @click="deleting = true">
          <Trash2 />删除
        </button>
      </div>
    </div>
    <section class="stats-grid project-stats">
      <div v-if="project.type==='artifact'" class="card stat-card">
        <Package /><span>发布版本</span><strong>{{ releases.length }}</strong>
      </div>
      <button v-if="project.type==='artifact'" class="card stat-card capacity-stat" @click="openCapacity">
        <HardDrive /><span>产物容量</span><strong>{{ formatBytes(project.artifact_bytes || 0) }}</strong><small>{{project.max_artifact_bytes ? `上限 ${formatBytes(project.max_artifact_bytes)}` : '不限容量'}}</small>
      </button>
      <div class="card stat-card">
        <Workflow /><span>工作流</span><strong>{{ workflows.length }}</strong>
      </div>
      <div class="card stat-card">
        <KeyRound /><span>项目 Token</span
        ><strong>{{ token?.prefix || "未启用" }}</strong>
      </div>
      <div class="card stat-card">
        <span>最近运行</span><strong>{{ runs[0]?.status || "暂无" }}</strong>
      </div>
    </section>
    <section class="project-workflows-section">
      <div class="section-head">
        <div>
          <h2>项目工作流</h2>
          <p>直接查看或进入画布编辑该项目的自动化流程。</p>
        </div>
        <router-link v-if="canDevelop" class="btn secondary" :to="`/workflows/editor/${id}?new=1`"><Plus/>新建工作流</router-link>
      </div>
      <div class="workflow-cards">
        <article v-for="flow in workflows" :key="flow.id" class="card workflow-card project-workflow-card">
          <div class="card-icon"><Workflow/></div>
          <div class="grow"><h3>{{flow.name}}</h3><p>{{flow.enabled?'已启用':'已停用'}} · {{workflowTriggerLabel(flow)}}</p><small>更新于 {{new Date(flow.updated_at).toLocaleString()}}</small></div>
          <div class="workflow-actions"><button v-if="canDevelop" class="btn secondary small-btn" @click="prepareRun(flow)"><Play/>运行</button><button class="btn secondary small-btn" @click="editWorkflow(flow)"><Pencil/>编辑</button><button v-if="canDevelop" class="btn secondary small-btn" @click="prepareCopyWorkflow(flow)"><Copy/>复制</button><button class="btn secondary small-btn" @click="openHistory(flow)"><History/>历史</button><button v-if="canDevelop" class="icon-btn danger-text" title="删除工作流" @click="deleteWorkflowTarget=flow"><Trash2/></button></div>
        </article>
        <EmptyState v-if="!workflows.length" title="暂无工作流" text="创建工作流后可自动处理发布产物。"><router-link v-if="canDevelop" class="btn" :to="`/workflows/editor/${id}?new=1`"><Plus/>创建工作流</router-link></EmptyState>
      </div>
    </section>
    <section v-if="project.type==='artifact'&&canAdmin" class="card token-card">
      <div class="section-head">
        <div>
          <h2>自动化访问</h2>
          <p>同一个 Token 用于项目产物上传和下载。</p>
        </div>
        <button class="btn secondary" @click="rotating = true">
          <KeyRound />轮换 Token
        </button>
      </div>
      <div class="meta-row">
        <span
          >前缀 <code>{{ token?.prefix || "—" }}</code></span
        ><span
          >最后使用
          {{
            token?.last_used_at
              ? new Date(token.last_used_at).toLocaleString()
              : "从未使用"
          }}</span
        >
      </div>
      <details class="code-disclosure">
        <summary><ChevronDown />GitHub / Gitea Action 示例</summary>
        <div class="code-card">
          <pre>{{ actionExample }}</pre>
          <button class="icon-btn" @click="copy(actionExample, 'action')">
            <Copy />
          </button>
        </div>
      </details>
    </section>
    <section v-if="project.type==='artifact'">
      <div class="section-head">
        <div>
          <h2>发布版本</h2>
          <p>点击卡片查看文件、摘要与下载入口。</p>
        </div>
        <span class="count">{{ releases.length }} 个版本</span>
      </div>
      <div class="release-list">
        <button
          v-for="r in releases"
          :key="r.id"
          class="card release-card"
          @click="detail(r)"
        >
          <div class="card-icon"><Package /></div>
          <div class="grow">
            <h3>{{ r.version }}</h3>
            <p class="release-reference"><span v-if="r.branch" class="branch-badge"><GitBranch/>{{r.branch}}</span><span>{{ r.ref_type || "未知来源" }} · {{ r.commit_sha?.slice(0, 10) || "无提交信息" }}</span></p>
            <small class="release-access"><Download/>访问 {{ r.access_count || 0 }} 次</small>
          </div>
          <time>{{ new Date(r.created_at).toLocaleString() }}</time></button
        ><EmptyState
          v-if="!releases.length"
          title="暂无产物"
          text="使用项目 Action 或上传接口提交第一个版本。"
        />
      </div></section><section class="card panel-card"><div class="section-head"><div><h2>项目成员</h2><p>通过固定角色分配项目权限。</p></div><button v-if="canAdmin" class="btn secondary" @click="memberOpen=true"><Plus/>添加成员</button></div><div class="member-list"><div v-for="m in members" :key="m.id" class="member-row"><Users/><div class="grow"><b>{{m.user?.username}}</b><small>{{m.user?.email||m.role}}</small></div><select v-if="canAdmin&&m.role!=='owner'" :value="m.role" @change="setRole(m,($event.target as HTMLSelectElement).value)"><option value="admin">管理员</option><option value="developer">开发者</option><option value="viewer">只读</option></select><span v-else class="badge">{{m.role}}</span><button v-if="canAdmin&&m.role!=='owner'" class="icon-btn" @click="removeMember(m)"><Trash2/></button></div></div></section><EnvironmentVariables v-if="canAdmin" :project-id="id" /></template
  ><Drawer v-model="selected" title="发布详情"
    ><template v-if="selected"
      ><div class="detail-list">
        <div>
          <span>版本</span><b>{{ selected.version }}</b>
        </div>
        <div>
          <span>类型</span><b>{{ selected.ref_type || "—" }}</b>
        </div>
        <div>
          <span>Commit</span><code>{{ selected.commit_sha || "—" }}</code>
        </div>
        <div>
          <span>分支</span><b>{{ selected.branch || "—" }}</b>
        </div>
      </div>
      <h3>产物文件</h3>
      <FileTree
        :files="selected.files || []"
        action="download"
        @select="downloadArtifact"
      />
      <div class="access-heading"><h3>访问记录</h3><span>{{ selected.access_count || 0 }} 次</span></div>
      <div class="access-list">
        <div v-for="record in accessRecords" :key="record.id" class="access-row">
          <Download/>
          <div class="grow"><b>{{ record.file_name }}</b><small>{{ record.ip_address }} · {{ record.access_method === 'project_token' ? '项目 Token' : '登录用户' }} · {{ record.http_method }}</small></div>
          <time>{{ new Date(record.created_at).toLocaleString() }}</time>
        </div>
        <EmptyState v-if="!accessRecords.length" title="暂无访问记录" text="产物下载成功后将在这里记录。"/>
      </div>
      </template></Drawer
  ><Modal
    v-model="capacityOpen"
    title="产物容量设置"
    description="达到上限后按发布时间自动清理最早的完整发布版本；最新版本始终保留。"
    ><label>最大容量<div class="capacity-input"><input v-model.number="capacityValue" type="number" min="0" step="0.1" placeholder="留空或 0 表示不限"><select v-model.number="capacityUnit"><option :value="1024**2">MB</option><option :value="1024**3">GB</option><option :value="1024**4">TB</option></select></div></label><div class="notice">当前已使用 {{formatBytes(project?.artifact_bytes||0)}}。保存更小的容量后会立即尝试清理旧版本。</div><div class="panel-actions"><button class="btn secondary" @click="capacityOpen=false">取消</button><button class="btn" :disabled="capacitySaving||!canAdmin" @click="saveCapacity"><Settings/>{{capacitySaving?'正在保存…':'保存设置'}}</button></div></Modal
  ><Modal
	 v-model="memberOpen" title="添加项目成员" description="输入已注册用户名并分配角色。"><label>用户名<input v-model="memberForm.username"></label><label>角色<select v-model="memberForm.role"><option value="admin" :disabled="project?.role!=='owner'">管理员</option><option value="developer">开发者</option><option value="viewer">只读</option></select></label><div class="panel-actions"><button class="btn" @click="addMember">添加成员</button></div></Modal><Modal
    v-model="rotating"
    title="轮换项目 Token"
    description="旧 Token 将立即失效，使用它的 CI 和下载链接会停止工作。"
    ><div class="panel-actions">
      <button class="btn secondary" @click="rotating = false">取消</button
      ><button class="btn danger" @click="rotate">确认轮换</button>
    </div></Modal
  ><Modal :model-value="!!runTarget" title="运行工作流" @update:model-value="runTarget=undefined" :description="runTarget?.name||''"><label v-if="project?.type==='artifact'">发布版本<select v-model.number="releaseID"><option :value="0" disabled>选择版本</option><option v-for="release in releases" :key="release.id" :value="release.id">{{release.version}}</option></select></label><div v-if="project?.type==='artifact'&&!releases.length" class="notice notice-warning">该项目尚无可用发布版本，暂时无法手动运行。</div><div class="panel-actions"><button class="btn secondary" @click="runTarget=undefined">取消</button><button class="btn" :disabled="running||(project?.type==='artifact'&&!releaseID)" @click="runWorkflow"><Play/>{{running?'正在创建…':'开始运行'}}</button></div></Modal
  ><Modal :model-value="!!historyTarget" title="运行历史" @update:model-value="historyTarget=undefined" :description="historyTarget?.flow.name||''"><div class="modal-run-list"><button v-for="item in historyTarget?.runs||[]" :key="item.id" class="run-history-row" @click="viewRun(item)"><span><b>运行 #{{item.id}}</b><small>{{new Date(item.created_at).toLocaleString()}}</small></span><StatusBadge :status="item.status"/><ArrowUpRight/></button><EmptyState v-if="historyTarget&&!historyTarget.runs.length" title="暂无运行记录"/></div></Modal
  ><Modal :model-value="!!copyTarget" title="复制工作流" @update:model-value="copyTarget=undefined" :description="copyTarget?`复制“${copyTarget.name}”的完整画布配置，不包含运行历史和定时计划。`:''"><label>目标项目<select v-model.number="copyForm.target_project_id"><option v-for="item in targetProjects.filter((item:any)=>['owner','admin','developer'].includes(item.role))" :key="item.id" :value="item.id">{{item.name}}</option></select></label><label>新工作流名称<input v-model.trim="copyForm.name" maxlength="120" placeholder="输入工作流名称"></label><div class="panel-actions"><button class="btn secondary" @click="copyTarget=undefined">取消</button><button class="btn" :disabled="running||!copyForm.target_project_id||!copyForm.name" @click="copyWorkflow"><Copy/>{{running?'正在复制…':'确认复制'}}</button></div></Modal
  ><Modal :model-value="!!deleteWorkflowTarget" title="删除工作流" @update:model-value="deleteWorkflowTarget=undefined" :description="deleteWorkflowTarget?`确认删除“${deleteWorkflowTarget.name}”？此操作不可撤销。`:''"><div class="panel-actions"><button class="btn secondary" @click="deleteWorkflowTarget=undefined">取消</button><button class="btn danger" @click="deleteWorkflow"><Trash2/>删除</button></div></Modal
  ><Modal
    :model-value="!!newToken"
    title="请保存新 Token"
    description="关闭后无法再次查看。"
    :locked="true"
    ><div class="token-box">
      <code>{{ newToken }}</code
      ><button class="icon-btn" @click="copy(newToken, 'token')">
        <Copy />
      </button>
    </div>
    <div class="panel-actions">
      <button class="btn" @click="newToken = ''">
        {{ copied === "token" ? "已复制" : "我已保存" }}
      </button>
    </div></Modal
  ><Modal
    v-model="deleting"
    title="删除项目"
    description="项目、发布文件与工作流将被永久删除，此操作不可撤销。"
    ><div class="panel-actions">
      <button class="btn secondary" @click="deleting = false">取消</button
      ><button class="btn danger" @click="remove">永久删除</button>
    </div></Modal
  ><RunDetailDrawer v-model="runDetailOpen" :project-id="runDetailProjectID" :run-id="runDetailID"/>
</template>
<style scoped>
.project-workflow-card { align-items: flex-start; flex-wrap: wrap; }
.project-workflow-card .workflow-actions { width: 100%; padding-top: 12px; border-top: 1px solid var(--border); justify-content: flex-start; }
.release-reference { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.branch-badge { display: inline-flex; align-items: center; gap: 4px; padding: 3px 7px; border-radius: 999px; background: var(--surface-2); color: var(--text); font-size: 11px; }
.branch-badge svg { width: 13px; height: 13px; }
.release-access { display: inline-flex; align-items: center; gap: 5px; margin-top: 5px; color: var(--muted); }
.release-access svg { width: 13px; height: 13px; }
.capacity-stat { width: 100%; text-align: left; cursor: pointer; color: inherit; font: inherit; }
.capacity-stat small { color: var(--muted); }
.capacity-input { display: grid; grid-template-columns: 1fr 88px; gap: 8px; margin-top: 7px; }
.access-heading { display: flex; align-items: center; justify-content: space-between; margin-top: 22px; }
.access-heading h3 { margin: 0; }
.access-heading span { color: var(--muted); font-size: 12px; }
.access-list { display: grid; gap: 8px; margin-top: 10px; }
.access-row { display: flex; align-items: center; gap: 10px; padding: 10px 12px; border: 1px solid var(--border); border-radius: 12px; }
.access-row > svg { width: 16px; height: 16px; flex: none; }
.access-row b, .access-row small { display: block; overflow-wrap: anywhere; }
.access-row small, .access-row time { color: var(--muted); font-size: 11px; }
.access-row time { flex: none; }
@media (max-width: 640px) { .access-row { align-items: flex-start; } .access-row time { display: none; } }
</style>
