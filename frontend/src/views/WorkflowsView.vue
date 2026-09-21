<script setup lang="ts">
// @ts-nocheck
import { computed, onMounted, onUnmounted, ref } from "vue";
import { useRoute } from "vue-router";
import { Handle, Position, VueFlow, useVueFlow } from "@vue-flow/core";
import "@vue-flow/core/dist/style.css";
import "@vue-flow/core/dist/theme-default.css";
import {
  Archive,
  FolderOpen,
  UploadCloud,
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
  Workflow as WorkflowIcon,
  Eye,
  File,
} from "lucide-vue-next";
import { api } from "../api";
import Modal from "../components/Modal.vue";
import Drawer from "../components/Drawer.vue";
import StatusBadge from "../components/StatusBadge.vue";
import EmptyState from "../components/EmptyState.vue";
const pid = Number(useRoute().params.id),
  flows = ref<any[]>([]),
  runs = ref<any[]>([]),
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
  createOpen = ref(false),
  runOpen = ref(false),
  releaseID = ref(0),
  previewReleaseID = ref(0),
  previewFiles = ref<any[]>([]),
  previewOpen = ref(false),
  saved = ref(true),
  history = ref<string[]>([]),
  future = ref<string[]>([]),
  error = ref("");
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
};
async function load() {
  [flows.value, runs.value, releases.value, connections.value] =
    await Promise.all([
      api(`/api/projects/${pid}/workflows`),
      api(`/api/projects/${pid}/runs`),
      api(`/api/projects/${pid}/releases`),
      api("/api/ssh-connections"),
    ]);
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
      files: previewFiles.value.filter((file) => regex.test(file.name)),
      error: "",
    };
  } catch (e: any) {
    return { files: [], error: e.message };
  }
});
const supportsPattern = computed(() =>
  ["archive", "extract", "sftp_upload", "checksum_verify"].includes(
    selected.value?.data?.module,
  ),
);
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
      module: type,
      config: structuredClone(m.defaults),
      timeout_seconds: 600,
      retries: 0,
    },
  });
}
function connect(e: any) {
  checkpoint();
  addEdges([{ ...e, id: `e_${Date.now()}` }]);
}
function choose(e: any) {
  selected.value = e.node;
}
function restore(raw: string) {
  const x = JSON.parse(raw);
  nodes.value = x.nodes;
  edges.value = x.edges;
  selected.value = undefined;
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
  if (!selected.value) return;
  checkpoint();
  nodes.value = nodes.value.filter((n) => n.id !== selected.value.id);
  edges.value = edges.value.filter(
    (e) => e.source !== selected.value.id && e.target !== selected.value.id,
  );
  selected.value = undefined;
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
}
async function edit(id: number) {
  const w = await api<any>(`/api/projects/${pid}/workflows/${id}`);
  editing.value = id;
  name.value = w.name;
  enabled.value = w.enabled;
  triggerType.value = w.trigger_type;
  triggerGlob.value = w.trigger_glob;
  nodes.value = w.definition.nodes.map((n: any) => ({
    id: n.id,
    label: modules[n.type]?.name || n.type,
    position: n.position || { x: 100, y: 100 },
    data: {
      module: n.type,
      config: n.config || {},
      timeout_seconds: n.timeout_seconds,
      retries: n.retries,
    },
  }));
  edges.value = w.definition.edges.map((e: any, i: number) => ({
    id: `e${i}`,
    source: e.from,
    target: e.to,
  }));
  history.value = [];
  future.value = [];
  saved.value = true;
}
async function save() {
  error.value = "";
  if (!name.value.trim() || !nodes.value.length) {
    error.value = "请填写名称并至少添加一个模块";
    return;
  }
  const definition = {
    nodes: nodes.value.map((n) => ({
      id: n.id,
      type: n.data.module,
      config: n.data.config,
      timeout_seconds: n.data.timeout_seconds,
      retries: n.data.retries,
      position: n.position,
    })),
    edges: edges.value.map((e) => ({ from: e.source, to: e.target })),
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
          definition,
        }),
      },
    );
    editing.value = w.id || editing.value;
    saved.value = true;
    await load();
  } catch (e: any) {
    error.value = e.message;
  }
}
async function run() {
  await api(`/api/projects/${pid}/workflows/${editing.value}/runs`, {
    method: "POST",
    body: JSON.stringify({ release_id: releaseID.value }),
  });
  runOpen.value = false;
  await load();
}
function inputFocus() {
  return ["INPUT", "TEXTAREA", "SELECT"].includes(
    document.activeElement?.tagName || "",
  );
}
function key(e: KeyboardEvent) {
  if (!editing.value && editing.value !== 0) return;
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") {
    e.preventDefault();
    save();
    return;
  }
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
onMounted(() => {
  load();
  document.addEventListener("keydown", key);
});
onUnmounted(() => document.removeEventListener("keydown", key));
const moduleInfo = computed(() =>
  selected.value ? modules[selected.value.data.module] : null,
);
function triggerLabel(w: any) {
  return (
    (
      {
        any: "任意版本",
        tag: "仅 Tag",
        commit: "仅 Commit",
        tag_glob: `Tag · ${w.trigger_glob}`,
      } as any
    )[w.trigger_type] || w.trigger_type
  );
}
</script>
<template>
  <div v-if="editing === undefined">
    <div class="page-heading">
      <div>
        <span class="eyebrow">自动化部署</span>
        <h1>工作流</h1>
        <p>通过可视化模块编排产物发布后的自动操作。</p>
      </div>
      <button class="btn" @click="beginCreate"><Plus />新建工作流</button>
    </div>
    <div class="workflow-cards">
      <article
        v-for="f in flows"
        :key="f.id"
        class="card workflow-card"
        @click="edit(f.id)"
      >
        <div class="card-icon"><WorkflowIcon /></div>
        <div class="grow">
          <div class="card-title">
            <h3>{{ f.name }}</h3>
            <StatusBadge
              :status="f.enabled ? 'success' : 'disabled'"
              :label="f.enabled ? '已启用' : '已停用'"
            />
          </div>
          <p>{{ triggerLabel(f) }}</p>
          <small>更新于 {{ new Date(f.updated_at).toLocaleString() }}</small>
        </div>
      </article>
      <EmptyState
        v-if="!flows.length"
        title="暂无工作流"
        text="用可视化模块创建第一条部署流程。"
        ><button class="btn" @click="beginCreate">
          <Plus />创建工作流
        </button></EmptyState
      >
    </div>
    <section class="section-spaced">
      <div class="section-head">
        <div>
          <h2>最近运行</h2>
          <p>此项目最近的执行记录</p>
        </div>
      </div>
      <div class="card run-list">
        <div v-for="r in runs.slice(0, 8)" :key="r.id" class="run-row">
          <b>#{{ r.id }}</b
          ><span class="grow">{{
            new Date(r.created_at).toLocaleString()
          }}</span
          ><StatusBadge :status="r.status" />
        </div>
        <EmptyState v-if="!runs.length" title="暂无运行记录" />
      </div>
    </section>
  </div>
  <div v-else class="workflow-editor">
    <header class="editor-toolbar">
      <button class="icon-btn" title="返回" @click="editing = undefined">
        <ArrowLeft />
      </button>
      <div class="editor-name">
        <input v-model="name" @input="saved = false" /><span>{{
          saved ? "已保存" : "有未保存更改"
        }}</span>
      </div>
      <label class="switch"
        ><input v-model="enabled" type="checkbox" />启用</label
      >
      <div class="release-preview-control">
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
      <button v-if="editing" class="btn secondary" @click="runOpen = true">
        <Play />运行</button
      ><button class="btn" @click="save"><Save />保存</button>
    </header>
    <p v-if="error" class="editor-error">{{ error }}</p>
    <div class="editor-body">
      <aside class="module-palette">
        <span class="eyebrow">模块</span
        ><button
          v-for="(m, type) in modules"
          :key="type"
          @click="add(type as string)"
        >
          <component :is="m.icon" /><span
            ><b>{{ m.name }}</b
            ><small>{{ m.desc }}</small></span
          ><Plus />
        </button>
      </aside>
      <div class="flow-canvas">
        <VueFlow
          v-model:nodes="nodes"
          v-model:edges="edges"
          fit-view-on-init
          @connect="connect"
          @node-click="choose"
          @pane-click="selected = undefined"
          ><template #node-default="p"
            ><div class="flow-node" :class="{ selected: p.selected }">
              <Handle
                id="input"
                type="target"
                :position="Position.Left"
                title="连接上一步"
              />
              <component :is="modules[p.data.module]?.icon" />
              <div>
                <b>{{ modules[p.data.module]?.name }}</b
                ><small>{{ p.id }}</small>
              </div>
              <Handle
                id="output"
                type="source"
                :position="Position.Right"
                title="连接下一步"
              /></div></template
        ></VueFlow>
        <div class="canvas-hint">拖动画布 · 滚轮缩放 · 点击节点配置</div>
      </div>
    </div>
  </div>
  <Drawer v-model="previewOpen" title="版本文件预览">
    <div class="release-preview-summary">
      <b>{{
        releases.find((item) => item.id === previewReleaseID)?.version
      }}</b>
      <span>{{ previewFiles.length }} 个文件</span>
    </div>
    <div class="preview-file-list">
      <div v-for="file in previewFiles" :key="file.id" class="preview-file-row">
        <File />
        <div>
          <b>{{ file.name }}</b>
          <small
            >{{ file.kind === "extracted" ? "解压文件" : "上传文件" }} ·
            {{ file.size }} bytes</small
          >
        </div>
      </div>
      <EmptyState v-if="!previewFiles.length" title="该版本没有文件" />
    </div>
  </Drawer>
  <Drawer
    :model-value="!!selected"
    :title="moduleInfo?.name || '节点配置'"
    @update:model-value="selected = undefined"
    ><template v-if="selected"
      ><p class="muted">{{ moduleInfo?.desc }}</p>
      <label>节点 ID<input v-model="selected.id" /></label>
      <div v-if="supportsPattern" class="pattern-picker">
        <label>
          文件正则表达式
          <input
            v-model="selected.data.config.file_pattern"
            placeholder="例如：^dist/.*\\.(js|css)$"
          />
        </label>
        <p v-if="!previewReleaseID" class="pattern-tip">
          请先在编辑器顶部选择一个版本，以实时预览匹配文件。
        </p>
        <p v-else-if="patternResult.error" class="error">
          正则表达式无效：{{ patternResult.error }}
        </p>
        <div v-else class="pattern-results">
          <div class="pattern-result-head">
            <span>实时匹配</span><b>{{ patternResult.files.length }} 个文件</b>
          </div>
          <button
            v-for="file in patternResult.files.slice(0, 20)"
            :key="file.id"
            type="button"
            @click="
              selected.data.config.file_pattern = `^${file.name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`
            "
          >
            <File />{{ file.name }}
          </button>
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
          ['sftp_upload', 'ssh_command'].includes(selected.data.module)
        "
        ><label
          >服务器连接<select
            v-model.number="selected.data.config.connection_id"
          >
            <option :value="0">选择连接</option>
            <option v-for="c in connections" :value="c.id">
              {{ c.name }} · {{ c.host }}
            </option>
          </select></label
        ><template v-if="selected.data.module === 'sftp_upload'"
          ><label
            >远端路径<input
              v-model="selected.data.config.destination" /></label></template
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
      ><template v-else-if="selected.data.module === 'http_webhook'"
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
        <button class="btn secondary" @click="duplicate"><Copy />复制</button
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
    ><label
      >触发条件<select v-model="triggerType">
        <option value="any">任意版本</option>
        <option value="tag">仅 Tag</option>
        <option value="commit">仅 Commit</option>
        <option value="tag_glob">Tag Glob</option>
      </select></label
    ><label v-if="triggerType === 'tag_glob'"
      >Tag 匹配规则<input v-model="triggerGlob" placeholder="v*"
    /></label>
    <div class="panel-actions">
      <button class="btn secondary" @click="createOpen = false">取消</button
      ><button class="btn" @click="startEditor">进入编辑器</button>
    </div></Modal
  ><Modal
    v-model="runOpen"
    title="手动运行"
    description="选择一个已有发布作为本次运行输入。"
    ><label
      >发布版本<select v-model.number="releaseID">
        <option :value="0" disabled>选择版本</option>
        <option v-for="r in releases" :value="r.id">{{ r.version }}</option>
      </select></label
    >
    <div class="panel-actions">
      <button class="btn secondary" @click="runOpen = false">取消</button
      ><button class="btn" :disabled="!releaseID" @click="run">
        <Play />开始运行
      </button>
    </div></Modal
  >
</template>
