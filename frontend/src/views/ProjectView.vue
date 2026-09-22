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
} from "lucide-vue-next";
import { api, authDownload } from "../api";
import Modal from "../components/Modal.vue";
import Drawer from "../components/Drawer.vue";
import EmptyState from "../components/EmptyState.vue";
import FileTree from "../components/FileTree.vue";
import EnvironmentVariables from "../components/EnvironmentVariables.vue";
const route = useRoute(),
  router = useRouter(),
  id = Number(route.params.id),
  project = ref<any>(),
  token = ref<any>(),
  releases = ref<any[]>([]),
  workflows = ref<any[]>([]),
  runs = ref<any[]>([]),
  selected = ref<any>(),
  rotating = ref(false),
  newToken = ref(""),
  deleting = ref(false),
  copied = ref("");
const members=ref<any[]>([]),memberOpen=ref(false),memberForm=ref({username:"",role:"viewer"});
async function load() {
  project.value=await api<any>(`/api/projects/${id}`);[workflows.value,runs.value,members.value]=await Promise.all([api<any[]>(`/api/projects/${id}/workflows`),api<any[]>(`/api/projects/${id}/runs`),api<any[]>(`/api/projects/${id}/members`)]);if(project.value.type==='artifact'){releases.value=await api<any[]>(`/api/projects/${id}/releases`);token.value=await api<any>(`/api/projects/${id}/token`).catch(()=>null)}else{releases.value=[];token.value=null}
}
const canAdmin=computed(()=>['owner','admin'].includes(project.value?.role)),canDevelop=computed(()=>['owner','admin','developer'].includes(project.value?.role));
async function addMember(){await api(`/api/projects/${id}/members`,{method:'POST',body:JSON.stringify(memberForm.value)});memberOpen.value=false;memberForm.value={username:'',role:'viewer'};await load()}
async function setRole(member:any,role:string){await api(`/api/projects/${id}/members/${member.user_id}`,{method:'PUT',body:JSON.stringify({role})});await load()}
async function removeMember(member:any){await api(`/api/projects/${id}/members/${member.user_id}`,{method:'DELETE'});await load()}
async function detail(r: any) {
  selected.value = await api(`/api/projects/${id}/releases/${r.id}`);
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
async function copy(text: string, key = "x") {
  await navigator.clipboard.writeText(text);
  copied.value = key;
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
        <router-link class="btn" :to="`/projects/${id}/workflows`"
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
            <p>
              {{ r.ref_type || "未知来源" }} ·
              {{ r.commit_sha?.slice(0, 10) || "无提交信息" }}
            </p>
          </div>
          <time>{{ new Date(r.created_at).toLocaleString() }}</time></button
        ><EmptyState
          v-if="!releases.length"
          title="暂无产物"
          text="使用项目 Action 或上传接口提交第一个版本。"
        />
      </div></section><section class="card"><div class="section-head"><div><h2>项目成员</h2><p>通过固定角色分配项目权限。</p></div><button v-if="canAdmin" class="btn secondary" @click="memberOpen=true"><Plus/>添加成员</button></div><div class="member-list"><div v-for="m in members" :key="m.id" class="member-row"><Users/><div class="grow"><b>{{m.user?.username}}</b><small>{{m.user?.email||m.role}}</small></div><select v-if="canAdmin&&m.role!=='owner'" :value="m.role" @change="setRole(m,($event.target as HTMLSelectElement).value)"><option value="admin">管理员</option><option value="developer">开发者</option><option value="viewer">只读</option></select><span v-else class="badge">{{m.role}}</span><button v-if="canAdmin&&m.role!=='owner'" class="icon-btn" @click="removeMember(m)"><Trash2/></button></div></div></section><EnvironmentVariables v-if="canAdmin" :project-id="id" /></template
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
      /> </template></Drawer
  ><Modal
	 v-model="memberOpen" title="添加项目成员" description="输入已注册用户名并分配角色。"><label>用户名<input v-model="memberForm.username"></label><label>角色<select v-model="memberForm.role"><option value="admin" :disabled="project?.role!=='owner'">管理员</option><option value="developer">开发者</option><option value="viewer">只读</option></select></label><div class="panel-actions"><button class="btn" @click="addMember">添加成员</button></div></Modal><Modal
    v-model="rotating"
    title="轮换项目 Token"
    description="旧 Token 将立即失效，使用它的 CI 和下载链接会停止工作。"
    ><div class="panel-actions">
      <button class="btn secondary" @click="rotating = false">取消</button
      ><button class="btn danger" @click="rotate">确认轮换</button>
    </div></Modal
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
  >
</template>
