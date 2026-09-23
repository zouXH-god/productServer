<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import { useRouter } from "vue-router";
import { Copy, FolderKanban, HardDrive, Package, Plus, RefreshCw, Settings, Trash2, Workflow } from "lucide-vue-next";
import { api } from "../api";
import EmptyState from "../components/EmptyState.vue";
import Modal from "../components/Modal.vue";
import { toast } from "../toast";

const router = useRouter();
const projects = ref<any[]>([]), showCreate = ref(false), name = ref(""), type = ref("artifact"), capacityGB = ref<number | null>(null);
const saving = ref(false), refreshing = ref(false), created = ref<any>(), copied = ref(false), error = ref("");
const capacityTarget = ref<any>(), capacityValue = ref<number | null>(null), capacityUnit = ref(1024 ** 3), deleteTarget = ref<any>();

async function load() { refreshing.value = true; try { projects.value = await api("/api/projects"); } finally { refreshing.value = false; } }
async function create() {
  saving.value = true; error.value = "";
  try {
    const result: any = await api("/api/projects", { method: "POST", body: JSON.stringify({ name: name.value, type: type.value, max_artifact_bytes: type.value === "artifact" && capacityGB.value ? Math.round(capacityGB.value * 1024 ** 3) : 0 }) });
    showCreate.value = false; name.value = ""; capacityGB.value = null; if (result.token) created.value = result; await load();
  } catch (e: any) { error.value = e.message; } finally { saving.value = false; }
}
function formatBytes(value: number) { if (!value) return "0 B"; const units = ["B", "KB", "MB", "GB", "TB"], index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1); return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`; }
function roleLabel(role: string) { return ({ owner: "所有者", admin: "管理员", developer: "开发者", viewer: "只读" } as any)[role] || role; }
function capacityPercent(project: any) { return project.max_artifact_bytes ? Math.min(100, project.artifact_bytes / project.max_artifact_bytes * 100) : 0; }
function openCapacity(project: any) { capacityTarget.value = project; const bytes = Number(project.max_artifact_bytes || 0); capacityUnit.value = bytes >= 1024 ** 4 ? 1024 ** 4 : bytes >= 1024 ** 3 ? 1024 ** 3 : 1024 ** 2; capacityValue.value = bytes ? Number((bytes / capacityUnit.value).toFixed(2)) : null; }
async function saveCapacity() { if (!capacityTarget.value) return; saving.value = true; try { await api(`/api/projects/${capacityTarget.value.id}`, { method: "PUT", body: JSON.stringify({ max_artifact_bytes: capacityValue.value ? Math.round(capacityValue.value * capacityUnit.value) : 0 }) }); capacityTarget.value = undefined; await load(); } finally { saving.value = false; } }
async function removeProject() { if (!deleteTarget.value) return; saving.value = true; try { await api(`/api/projects/${deleteTarget.value.id}`, { method: "DELETE" }); deleteTarget.value = undefined; await load(); } finally { saving.value = false; } }
async function copy() { await navigator.clipboard.writeText(created.value.token); copied.value = true; toast("Token 复制成功"); setTimeout(() => copied.value = false, 1500); }
function key(e: KeyboardEvent) { if (e.key.toLowerCase() === "n" && !["INPUT", "TEXTAREA", "SELECT"].includes((document.activeElement?.tagName || ""))) showCreate.value = true; }
onMounted(() => { load(); document.addEventListener("keydown", key); }); onUnmounted(() => document.removeEventListener("keydown", key));
</script>

<template>
  <div class="page-heading"><div><span class="eyebrow">交付空间</span><h1>项目</h1><p>管理 CI 产物、定时任务与部署工作流。</p></div><div class="actions"><button class="btn secondary" :disabled="refreshing" @click="load"><RefreshCw :class="{spinning:refreshing}"/>刷新</button><button class="btn" @click="showCreate=true"><Plus/>新建项目</button></div></div>
  <div class="project-grid expanded-project-grid">
    <article v-for="p in projects" :key="p.id" class="card project-summary-card" role="link" tabindex="0" @click="router.push(`/projects/${p.id}`)" @keydown.enter.self="router.push(`/projects/${p.id}`)" @keydown.space.self.prevent="router.push(`/projects/${p.id}`)">
      <header><div class="card-icon"><FolderKanban/></div><div class="grow"><div class="project-title"><h3>{{p.name}}</h3><span class="badge">{{p.type==='scheduled'?'定时项目':'产物项目'}}</span></div><p>{{roleLabel(p.role)}} · 创建于 {{new Date(p.created_at).toLocaleDateString()}}</p></div></header>
      <div class="project-summary-grid"><div><Package/><span>最后版本</span><b>{{p.type==='artifact'?(p.latest_version||'暂无发布'):'不适用'}}</b><small v-if="p.latest_release_at">{{new Date(p.latest_release_at).toLocaleString()}}</small></div><div><Workflow/><span>工作流</span><b>{{p.workflow_count}} 条</b><small v-if="p.type==='artifact'">{{p.release_count}} 个发布版本</small></div></div>
      <div v-if="p.type==='artifact'" class="capacity-block"><div><span><HardDrive/>产物容量</span><b>{{formatBytes(p.artifact_bytes)}} / {{p.max_artifact_bytes?formatBytes(p.max_artifact_bytes):'不限'}}</b></div><div class="capacity-track"><i :style="{width:`${capacityPercent(p)}%`}"/></div></div>
      <footer v-if="p.role==='owner'||(p.type==='artifact'&&p.role==='admin')"><button v-if="p.type==='artifact'&&['owner','admin'].includes(p.role)" class="btn secondary small-btn" @click.stop="openCapacity(p)"><Settings/>容量设置</button><button v-if="p.role==='owner'" class="icon-btn danger-text" title="删除项目" @click.stop="deleteTarget=p"><Trash2/></button></footer>
    </article>
    <EmptyState v-if="!projects.length&&!refreshing" title="还没有项目" text="创建产物项目或定时项目开始使用。"><button class="btn" @click="showCreate=true"><Plus/>新建项目</button></EmptyState>
  </div>
  <Modal v-model="showCreate" title="新建项目" description="项目类型创建后不可更改。"><form @submit.prevent="create"><label>项目名称<input v-model="name" autofocus required maxlength="150" placeholder="例如：Web 控制台"></label><label>项目类型<select v-model="type"><option value="artifact">产物项目 · CI 上传与发布</option><option value="scheduled">定时项目 · Cron 触发工作流</option></select></label><label v-if="type==='artifact'">产物最大容量（GB）<input v-model.number="capacityGB" type="number" min="0" step="0.1" placeholder="留空表示不限容量"><small>达到上限后自动清理最早的发布版本。</small></label><p v-if="error" class="error">{{error}}</p><div class="panel-actions"><button type="button" class="btn secondary" @click="showCreate=false">取消</button><button class="btn" :disabled="saving">{{saving?'创建中…':'创建项目'}}</button></div></form></Modal>
  <Modal :model-value="!!capacityTarget" title="产物容量设置" @update:model-value="capacityTarget=undefined" :description="capacityTarget?.name||''"><label>最大容量<div class="capacity-input"><input v-model.number="capacityValue" type="number" min="0" step="0.1" placeholder="留空或 0 表示不限"><select v-model.number="capacityUnit"><option :value="1024**2">MB</option><option :value="1024**3">GB</option><option :value="1024**4">TB</option></select></div></label><p class="muted">当前使用 {{formatBytes(capacityTarget?.artifact_bytes||0)}}。降低容量后将立即尝试清理旧版本。</p><div class="panel-actions"><button class="btn secondary" @click="capacityTarget=undefined">取消</button><button class="btn" :disabled="saving" @click="saveCapacity">{{saving?'保存中…':'保存设置'}}</button></div></Modal>
  <Modal :model-value="!!deleteTarget" title="删除项目" @update:model-value="deleteTarget=undefined" :description="deleteTarget?`确认永久删除“${deleteTarget.name}”及其产物、工作流和运行记录？`:''"><div class="panel-actions"><button class="btn secondary" @click="deleteTarget=undefined">取消</button><button class="btn danger" :disabled="saving" @click="removeProject"><Trash2/>{{saving?'删除中…':'永久删除'}}</button></div></Modal>
  <Modal :model-value="!!created" title="请立即保存项目 Token" description="该 Token 仅显示一次，关闭后无法再次查看。" :locked="true"><div class="token-box"><code>{{created?.token}}</code><button class="icon-btn" title="复制" @click="copy"><Copy/></button></div><p class="muted">Token 可用于上传和直链下载。请保存到 CI Secret 或安全的密码管理器中。</p><div class="panel-actions"><button class="btn" @click="created=undefined">{{copied?'已复制':'我已保存'}}</button></div></Modal>
</template>

<style scoped>
.expanded-project-grid{grid-template-columns:repeat(auto-fit,minmax(350px,1fr))}.project-summary-card{padding:18px;display:grid;gap:17px;cursor:pointer;transition:transform .16s,border-color .16s,box-shadow .16s}.project-summary-card:hover{transform:translateY(-2px);border-color:#aaa}.project-summary-card:focus-visible{outline:3px solid #0002;outline-offset:3px}.project-summary-card header{display:flex;align-items:center;gap:13px}.project-title{display:flex;align-items:center;gap:8px}.project-title h3{margin:0}.project-summary-grid{display:grid;grid-template-columns:1fr 1fr;border:1px solid var(--border);border-radius:13px;overflow:hidden}.project-summary-grid>div{padding:12px;display:grid;grid-template-columns:auto 1fr;gap:3px 7px}.project-summary-grid>div+div{border-left:1px solid var(--border)}.project-summary-grid svg{width:15px;grid-row:1/4;color:var(--muted)}.project-summary-grid span,.project-summary-grid small{font-size:11px;color:var(--muted)}.project-summary-grid b{font-size:13px;overflow-wrap:anywhere}.capacity-block{display:grid;gap:8px}.capacity-block>div:first-child{display:flex;justify-content:space-between;gap:12px;font-size:12px}.capacity-block span{display:flex;align-items:center;gap:6px;color:var(--muted)}.capacity-block svg{width:15px}.capacity-track{height:6px;background:var(--surface-2);border-radius:99px;overflow:hidden}.capacity-track i{display:block;height:100%;background:#555;border-radius:99px;transition:width .2s}.project-summary-card footer{display:flex;align-items:center;gap:8px;border-top:1px solid var(--border);padding-top:14px;min-height:49px}.project-summary-card footer .danger-text{margin-left:auto}.capacity-input{display:grid;grid-template-columns:1fr 88px;gap:8px}.spinning{animation:spin .8s linear infinite}@keyframes spin{to{transform:rotate(360deg)}}@media(max-width:480px){.expanded-project-grid{grid-template-columns:1fr}.project-summary-grid{grid-template-columns:1fr}.project-summary-grid>div+div{border-left:0;border-top:1px solid var(--border)}.project-summary-card footer{flex-wrap:wrap}}
</style>
