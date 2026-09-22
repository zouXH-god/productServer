<script setup lang="ts">
defineOptions({ name: "FileTreeNode" });
import { ref } from "vue";
import {
  ChevronRight,
  File,
  Folder,
  FolderOpen,
  Download,
} from "lucide-vue-next";

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
            · {{ node.file.size }} bytes</template
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
