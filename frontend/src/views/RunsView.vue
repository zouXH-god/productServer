<script setup lang="ts">
import { onMounted, ref, watch } from "vue";
import { Activity, RefreshCw } from "lucide-vue-next";
import { useRoute, useRouter } from "vue-router";
import { api } from "../api";
import EmptyState from "../components/EmptyState.vue";
import RunDetailDrawer from "../components/RunDetailDrawer.vue";
import StatusBadge from "../components/StatusBadge.vue";

const route = useRoute();
const router = useRouter();
const runs = ref<any[]>([]);
const projects = ref<any[]>([]);
const workflows = ref<any[]>([]);
const status = ref("");
const projectID = ref(Number(route.query.project) || 0);
const workflowID = ref(Number(route.query.workflow) || 0);
const loading = ref(false);
const detailOpen = ref(false);
const detailProjectID = ref<number>();
const detailRunID = ref<number>();

async function load() {
  loading.value = true;
  const params = new URLSearchParams();
  if (status.value) params.set("status", status.value);
  if (projectID.value) params.set("project_id", String(projectID.value));
  if (workflowID.value) params.set("workflow_id", String(workflowID.value));
  try {
    runs.value = await api<any[]>(`/api/runs${params.size ? `?${params}` : ""}`);
  } finally {
    loading.value = false;
  }
}
async function loadWorkflows() {
  workflows.value = projectID.value ? await api<any[]>(`/api/projects/${projectID.value}/workflows`) : [];
}
function show(run: any) {
  detailProjectID.value = run.project_id;
  detailRunID.value = run.id;
  detailOpen.value = true;
}
function duration(start: any, end: any) {
  if (!start) return "—";
  const ms = Math.max(0, new Date(end || Date.now()).getTime() - new Date(start).getTime());
  if (ms < 1000) return `${ms}ms`;
  const seconds = Math.floor(ms / 1000);
  return seconds < 60 ? `${seconds}s` : `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
}

watch(projectID, async () => { workflowID.value = 0; await loadWorkflows(); });
watch([status, projectID, workflowID], load);
watch(detailOpen, open => {
  if (!open && route.query.run) {
    router.replace({
      path: "/runs",
      query: {
        ...(projectID.value ? { project: String(projectID.value) } : {}),
        ...(workflowID.value ? { workflow: String(workflowID.value) } : {})
      }
    });
  }
});
onMounted(async () => {
  projects.value = await api<any[]>("/api/projects");
  await loadWorkflows();
  await load();
  const project = Number(route.query.project), run = Number(route.query.run);
  if (project && run) show({ project_id: project, id: run });
});
</script>

<template>
  <div class="page-heading"><div><span class="eyebrow">自动化</span><h1>运行记录</h1><p>查看部署状态、执行图、节点日志和持久化输出。</p></div><button class="btn secondary" @click="load"><RefreshCw/>刷新</button></div>
  <section class="card"><div class="filter-bar"><label>项目<select v-model.number="projectID"><option :value="0">全部项目</option><option v-for="project in projects" :key="project.id" :value="project.id">{{project.name}}</option></select></label><label>工作流<select v-model.number="workflowID" :disabled="!projectID"><option :value="0">全部工作流</option><option v-for="flow in workflows" :key="flow.id" :value="flow.id">{{flow.name}}</option></select></label><label>状态<select v-model="status"><option value="">全部状态</option><option value="queued">等待中</option><option value="running">运行中</option><option value="succeeded">成功</option><option value="failed">失败</option><option value="cancelled">已取消</option></select></label></div><div v-if="loading" class="skeleton list-skeleton"></div><div v-else class="run-list"><button v-for="run in runs" :key="run.id" class="run-row run-row-button" @click="show(run)"><span class="card-icon"><Activity/></span><div class="run-main"><b>{{run.workflow_name||'工作流'}}</b><span>{{run.project_name}} · {{run.trigger_source==='schedule'?'定时计划':run.trigger_source==='manual'?'手动运行':run.version}} · #{{run.id}}</span></div><span>{{run.completed_nodes}} / {{run.node_count}} 节点</span><span>{{duration(run.started_at,run.finished_at)}}</span><StatusBadge :status="run.status"/></button><EmptyState v-if="!runs.length" title="没有符合条件的运行"/></div></section>
  <RunDetailDrawer v-model="detailOpen" :project-id="detailProjectID" :run-id="detailRunID"/>
</template>
