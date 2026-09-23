<script setup lang="ts">
defineOptions({ name: "FileTreeNode" });
import { ref } from "vue";
import {
  ChevronRight,
  File,
  Folder,
  FolderOpen,
  Download,
  Copy,
  Check,
} from "lucide-vue-next";
import { toast } from "../toast";

export type FileTreeItem = {
  name: string;
  path: string;
  directory: boolean;
  children: FileTreeItem[];
  file?: any;
};

const props = withDefaults(
  defineProps<{
    node: FileTreeItem;
    depth?: number;
    action?: "download" | "select" | "none";
  }>(),
  { depth: 0, action: "none" },
);
const emit = defineEmits<{ select: [file: any] }>();
const open = ref(props.depth < 2);
const copied = ref(false);
function artifactPath() {
  return String(props.node.file?.name || props.node.path)
    .replaceAll("\\", "/")
    .replace(/^\/+/, "");
}
async function copyPath() {
  const path = artifactPath();
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(path);
  } else {
    const input = document.createElement("textarea");
    input.value = path;
    input.style.position = "fixed";
    input.style.opacity = "0";
    document.body.appendChild(input);
    input.select();
    document.execCommand("copy");
    input.remove();
  }
  copied.value = true;
  toast(`已复制路径：${path}`);
  window.setTimeout(() => (copied.value = false), 1400);
}
function formatFileSize(value: unknown) {
  const bytes = Number(value);
  if (!Number.isFinite(bytes) || bytes < 0) return "—";
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let size = bytes;
  let unit = -1;
  do {
    size /= 1024;
    unit += 1;
  } while (size >= 1024 && unit < units.length - 1);
  const digits = size >= 100 ? 0 : size >= 10 ? 1 : 2;
  return `${Number(size.toFixed(digits))} ${units[unit]}`;
}
</script>

<template>
  <div class="file-tree-node">
    <button
      v-if="node.directory"
      type="button"
      class="file-tree-row folder-row"
      :style="{ paddingLeft: `${7 + depth * 16}px` }"
      @click="open = !open"
    >
      <ChevronRight class="tree-chevron" :class="{ open }" />
      <FolderOpen v-if="open" />
      <Folder v-else />
      <b>{{ node.name }}</b>
      <small>{{ node.children.length }} 项</small>
    </button>
    <div
      v-else
      class="file-tree-row"
      :class="{ selectable: action === 'select' }"
      :style="{ paddingLeft: `${7 + depth * 16}px` }"
      @click="action === 'select' && emit('select', node.file)"
    >
      <span class="tree-spacer"></span>
      <File />
      <div class="file-tree-info">
        <b>{{ node.name }}</b>
        <small>
          {{ node.file?.kind === "extracted" ? "解压文件" : "上传文件" }}
          <template v-if="node.file?.size !== undefined">
            · {{ formatFileSize(node.file.size) }}</template
          >
        </small>
      </div>
      <button
        v-if="action === 'download'"
        type="button"
        class="icon-btn"
        title="下载文件"
        @click.stop="emit('select', node.file)"
      >
        <Download />
      </button>
      <button
        type="button"
        class="icon-btn copy-path-button"
        :title="copied ? '已复制产物路径' : `复制产物路径：${artifactPath()}`"
        :aria-label="copied ? '已复制产物路径' : `复制产物路径 ${artifactPath()}`"
        @click.stop="copyPath"
      >
        <Check v-if="copied" />
        <Copy v-else />
      </button>
    </div>
    <div v-if="node.directory && open" class="file-tree-children">
      <FileTreeNode
        v-for="child in node.children"
        :key="child.path"
        :node="child"
        :depth="depth + 1"
        :action="action"
        @select="emit('select', $event)"
      />
    </div>
  </div>
</template>
