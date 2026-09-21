<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  Workflow,
  KeyRound,
  Package,
  Trash2,
  Copy,
  Download,
  ChevronDown,
} from "lucide-vue-next";
import { api, authDownload } from "../api";
import Modal from "../components/Modal.vue";
import Drawer from "../components/Drawer.vue";
import EmptyState from "../components/EmptyState.vue";
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
async function load() {
  [project.value, releases.value, workflows.value, runs.value] =
    await Promise.all([
      api<any>(`/api/projects/${id}`),
      api<any[]>(`/api/projects/${id}/releases`),
      api<any[]>(`/api/projects/${id}/workflows`),
      api<any[]>(`/api/projects/${id}/runs`),
    ]);
  token.value = await api<any>(`/api/projects/${id}/token`).catch(() => null);
}
async function detail(r: any) {
  selected.value = await api(`/api/projects/${id}/releases/${r.id}`);
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
        <p>创建于 {{ new Date(project.created_at).toLocaleString() }}</p>
      </div>
      <div class="actions">
        <router-link class="btn" :to="`/projects/${id}/workflows`"
          ><Workflow />工作流</router-link
        ><button class="btn danger-ghost" @click="deleting = true">
          <Trash2 />删除
        </button>
      </div>
    </div>
    <section class="stats-grid project-stats">
      <div class="card stat-card">
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
    <section class="card token-card">
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
    <section>
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
      </div></section></template
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
      <div v-for="f in selected.files" :key="f.id" class="file-row">
        <div>
          <b>{{ f.name }}</b>
          <span class="artifact-kind" :class="`artifact-${f.kind || 'uploaded'}`">
            {{ f.kind === "extracted" ? "已解压" : "压缩包 / 上传文件" }}
          </span
          ><small
            >{{ (f.size / 1024).toFixed(1) }} KB ·
            {{ f.sha256.slice(0, 12) }}…</small
          >
        </div>
        <button
          class="icon-btn"
          title="下载"
          @click="
            authDownload(
              `/api/projects/${id}/releases/${selected.id}/files/${f.id}/download`,
              f.name,
            )
          "
        >
          <Download />
        </button></div></template></Drawer
  ><Modal
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
