<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { Workflow, Plus, Play, Pencil, Trash2, History, ArrowUpRight, FolderKanban, CalendarClock, Copy } from "lucide-vue-next";
import { api } from "../api";
import EmptyState from "../components/EmptyState.vue";
import Modal from "../components/Modal.vue";
import RunDetailDrawer from "../components/RunDetailDrawer.vue";
import StatusBadge from "../components/StatusBadge.vue";

const router = useRouter();
const groups = ref<any[]>([]), loading = ref(true), error = ref("");
const runTarget = ref<any>(), historyTarget = ref<any>(), deleteTarget = ref<any>();
const scheduleTarget = ref<any>();
const scheduleForm = ref({ cron: "0 * * * *", timezone: "Asia/Shanghai", enabled: true, allow_parallel: false });
const releases = ref<any[]>([]), releaseID = ref(0), busy = ref(false);
const detailOpen = ref(false), detailProjectID = ref<number>(), detailRunID = ref<number>();
const copyTarget = ref<any>(), copyForm = ref({ target_project_id: 0, name: "" });
const canDevelop = (project: any) => ["owner", "admin", "developer"].includes(project.role);
function triggerLabel(flow: any) { if (flow.trigger_type === "tag_glob") return `Tag ${flow.trigger_glob || "v*"}`; if(flow.trigger_type==="branch_glob")return `分支 ${flow.trigger_glob||"main"}`; return ({ any: "任意版本", tag: "仅 Tag", commit: "所有分支 Commit" } as any)[flow.trigger_type] || flow.trigger_type; }
async function load() {
  loading.value = true; error.value = "";
  try {
    const projects = await api<any[]>("/api/projects");
    groups.value = await Promise.all(projects.map(async project => {
      const [workflows, runs] = await Promise.all([api<any[]>(`/api/projects/${project.id}/workflows`), api<any[]>(`/api/projects/${project.id}/runs`)]);
      return { project, workflows, runs };
    }));
  } catch (e: any) { error.value = e.message; } finally { loading.value = false; }
}
function edit(project: any, flow: any) { router.push({ path: `/workflows/editor/${project.id}`, query: { workflow: String(flow.id) } }); }
function prepareCopy(group:any,flow:any){copyTarget.value={group,flow};copyForm.value={target_project_id:group.project.id,name:`${flow.name} 副本`}}
async function copyWorkflow(){if(!copyTarget.value)return;busy.value=true;try{const {group,flow}=copyTarget.value;await api(`/api/projects/${group.project.id}/workflows/${flow.id}/copy`,{method:'POST',body:JSON.stringify(copyForm.value)});copyTarget.value=undefined;await load()}finally{busy.value=false}}
async function prepareRun(group: any, flow: any) {
  runTarget.value = { group, flow }; releaseID.value = 0;
  releases.value = group.project.type === "artifact" ? await api<any[]>(`/api/projects/${group.project.id}/releases`) : [];
  if (releases.value.length) releaseID.value = releases.value[0].id;
}
async function run() {
  if (!runTarget.value) return; busy.value = true;
  try {
    const { group, flow } = runTarget.value;
    const created = await api<any>(`/api/projects/${group.project.id}/workflows/${flow.id}/runs`, { method: "POST", body: JSON.stringify({ release_id: releaseID.value }) });
    runTarget.value = undefined;
    await router.push({ path: "/runs", query: { project: String(group.project.id), workflow: String(flow.id), run: String(created.id) } });
  } finally { busy.value = false; }
}
function openHistory(group: any, flow: any) { historyTarget.value = { group, flow, runs: group.runs.filter((item: any) => item.workflow_id === flow.id) }; }
function openSchedule(group: any, flow: any) { const schedule = flow.schedule; scheduleTarget.value = { group, flow }; scheduleForm.value = { cron: schedule?.cron || "0 * * * *", timezone: schedule?.timezone || "Asia/Shanghai", enabled: schedule?.enabled ?? true, allow_parallel: schedule?.allow_parallel ?? false }; }
async function saveSchedule() { const { group, flow } = scheduleTarget.value; await api(`/api/projects/${group.project.id}/workflows/${flow.id}/schedule`, { method: "PUT", body: JSON.stringify(scheduleForm.value) }); scheduleTarget.value = undefined; await load(); }
async function triggerSchedule() { const { group, flow } = scheduleTarget.value; const created = await api<any>(`/api/projects/${group.project.id}/workflows/${flow.id}/schedule/trigger`, { method: "POST" }); scheduleTarget.value = undefined; if (created?.run?.id) await router.push({ path: "/runs", query: { project: String(group.project.id), workflow: String(flow.id), run: String(created.run.id) } }); else await load(); }
function viewRun(item: any) { const target = historyTarget.value; if (!target) return; detailProjectID.value = target.group.project.id; detailRunID.value = item.id; historyTarget.value = undefined; detailOpen.value = true; }
async function remove() { if (!deleteTarget.value) return; const { group, flow } = deleteTarget.value; await api(`/api/projects/${group.project.id}/workflows/${flow.id}`, { method: "DELETE" }); deleteTarget.value = undefined; await load(); }
onMounted(load);
</script>

<template>
  <div class="page-heading"><div><span class="eyebrow">自动化部署</span><h1>工作流</h1><p>按项目集中查看、运行和维护所有自动化流程。</p></div></div>
  <p v-if="error" class="notice notice-danger">{{ error }}</p>
  <div v-if="loading" class="workflow-hub"><div v-for="n in 3" :key="n" class="card skeleton hub-skeleton"></div></div>
  <div v-else class="workflow-hub">
    <section v-for="group in groups" :key="group.project.id" class="workflow-project-group">
      <header class="workflow-project-head">
        <router-link :to="`/projects/${group.project.id}`"><span class="card-icon"><FolderKanban/></span><span><b>{{group.project.name}}</b><small>{{group.project.type==='scheduled'?'定时项目':'产物项目'}} · {{group.workflows.length}} 条工作流</small></span><ArrowUpRight/></router-link>
        <router-link v-if="canDevelop(group.project)" class="btn secondary small-btn" :to="`/workflows/editor/${group.project.id}?new=1`"><Plus/>新建</router-link>
      </header>
      <div class="workflow-list card">
        <article v-for="flow in group.workflows" :key="flow.id" class="workflow-list-row">
          <span class="card-icon"><Workflow/></span>
          <div class="grow"><div class="workflow-name"><b>{{flow.name}}</b><StatusBadge :status="flow.enabled?'success':'disabled'" :label="flow.enabled?'已启用':'已停用'"/></div><small>{{group.project.type==='scheduled'?(flow.schedule?.cron?`${flow.schedule.cron} · ${flow.schedule.timezone}`:'未配置计划'):triggerLabel(flow)}} · 更新于 {{new Date(flow.updated_at).toLocaleString()}}</small></div>
          <div class="workflow-actions"><button v-if="canDevelop(group.project)" class="btn secondary small-btn" @click="prepareRun(group,flow)"><Play/>运行</button><button class="btn secondary small-btn" @click="edit(group.project,flow)"><Pencil/>编辑</button><button v-if="canDevelop(group.project)" class="btn secondary small-btn" @click="prepareCopy(group,flow)"><Copy/>复制</button><button class="btn secondary small-btn" @click="openHistory(group,flow)"><History/>运行历史</button><button v-if="group.project.type==='scheduled'&&canDevelop(group.project)" class="btn secondary small-btn" @click="openSchedule(group,flow)"><CalendarClock/>定时设置</button><button v-if="canDevelop(group.project)" class="icon-btn danger-text" title="删除工作流" @click="deleteTarget={group,flow}"><Trash2/></button></div>
        </article>
        <EmptyState v-if="!group.workflows.length" title="暂无工作流" text="此项目还没有配置自动化流程。"/>
      </div>
    </section>
    <EmptyState v-if="!groups.length" title="还没有项目" text="创建项目后即可配置部署工作流。"><router-link class="btn" to="/projects">创建项目</router-link></EmptyState>
  </div>
  <Modal :model-value="!!runTarget" title="运行工作流" @update:model-value="runTarget=undefined" :description="runTarget?`${runTarget.group.project.name} · ${runTarget.flow.name}`:''"><label v-if="runTarget?.group.project.type==='artifact'">发布版本<select v-model.number="releaseID"><option :value="0" disabled>选择版本</option><option v-for="release in releases" :key="release.id" :value="release.id">{{release.version}}</option></select></label><div v-if="runTarget?.group.project.type==='artifact'&&!releases.length" class="notice notice-warning">该项目尚无可用发布版本，暂时无法手动运行。</div><div class="panel-actions"><button class="btn secondary" @click="runTarget=undefined">取消</button><button class="btn" :disabled="busy||(runTarget?.group.project.type==='artifact'&&!releaseID)" @click="run"><Play/>{{busy?'正在创建…':'开始运行'}}</button></div></Modal>
  <Modal :model-value="!!historyTarget" title="运行历史" @update:model-value="historyTarget=undefined" :description="historyTarget?`${historyTarget.group.project.name} · ${historyTarget.flow.name}`:''"><div class="modal-run-list"><button v-for="item in historyTarget?.runs||[]" :key="item.id" class="run-history-row" @click="viewRun(item)"><span><b>运行 #{{item.id}}</b><small>{{new Date(item.created_at).toLocaleString()}}</small></span><StatusBadge :status="item.status"/><ArrowUpRight/></button><EmptyState v-if="historyTarget&&!historyTarget.runs.length" title="暂无运行记录"/></div></Modal>
  <Modal :model-value="!!copyTarget" title="复制工作流" @update:model-value="copyTarget=undefined" :description="copyTarget?`复制“${copyTarget.flow.name}”的完整画布配置，不包含运行历史和定时计划。`:''"><label>目标项目<select v-model.number="copyForm.target_project_id"><option v-for="group in groups.filter(item=>canDevelop(item.project))" :key="group.project.id" :value="group.project.id">{{group.project.name}}</option></select></label><label>新工作流名称<input v-model.trim="copyForm.name" maxlength="120" placeholder="输入工作流名称"></label><div class="panel-actions"><button class="btn secondary" @click="copyTarget=undefined">取消</button><button class="btn" :disabled="busy||!copyForm.target_project_id||!copyForm.name" @click="copyWorkflow"><Copy/>{{busy?'正在复制…':'确认复制'}}</button></div></Modal>
  <Modal :model-value="!!scheduleTarget" title="工作流定时计划" @update:model-value="scheduleTarget=undefined" description="使用标准五段 Cron 和 IANA 时区。"><label>快捷计划<select @change="scheduleForm.cron=($event.target as HTMLSelectElement).value"><option value="0 * * * *">每小时</option><option value="0 0 * * *">每天 00:00</option><option value="0 0 * * 1">每周一 00:00</option></select></label><label>Cron<input v-model="scheduleForm.cron" placeholder="0 * * * *"></label><label>时区<input v-model="scheduleForm.timezone" placeholder="Asia/Shanghai"></label><label class="checkbox-row"><input v-model="scheduleForm.enabled" type="checkbox">启用计划</label><label class="checkbox-row"><input v-model="scheduleForm.allow_parallel" type="checkbox">允许同一工作流并行运行</label><div class="panel-actions"><button class="btn secondary" @click="triggerSchedule">立即测试</button><button class="btn" @click="saveSchedule">保存计划</button></div></Modal>
  <Modal :model-value="!!deleteTarget" title="删除工作流" @update:model-value="deleteTarget=undefined" :description="deleteTarget?`确认删除“${deleteTarget.flow.name}”？此操作不可撤销。`:''"><div class="panel-actions"><button class="btn secondary" @click="deleteTarget=undefined">取消</button><button class="btn danger" @click="remove"><Trash2/>删除</button></div></Modal>
  <RunDetailDrawer v-model="detailOpen" :project-id="detailProjectID" :run-id="detailRunID"/>
</template>
