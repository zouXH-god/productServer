<script setup lang="ts">
import { computed } from "vue";
import FileTreeNode, { type FileTreeItem } from "./FileTreeNode.vue";

const props = withDefaults(
  defineProps<{ files: any[]; action?: "download" | "select" | "none" }>(),
  { action: "none" },
);
const emit = defineEmits<{ select: [file: any] }>();

const tree = computed(() => {
  const root: FileTreeItem[] = [];
  for (const file of props.files || []) {
    const namespace = file.kind === "extracted" ? "files" : "archive";
    const parts = `${namespace}/${String(file.name || "")}`
      .replaceAll("\\", "/")
      .split("/")
      .filter(Boolean);
    let level = root;
    let currentPath = "";
    parts.forEach((part, index) => {
      currentPath = currentPath ? `${currentPath}/${part}` : part;
      const directory = index < parts.length - 1;
      let node = level.find(
        (item) => item.name === part && item.directory === directory,
      );
      if (!node) {
        node = {
          name: part,
          path: currentPath,
          directory,
          children: [],
          file: directory ? undefined : file,
        };
        level.push(node);
      }
      level = node.children;
    });
  }
  const sort = (items: FileTreeItem[]) => {
    items.sort(
      (a, b) =>
        Number(b.directory) - Number(a.directory) ||
        a.name.localeCompare(b.name),
    );
    items.forEach((item) => sort(item.children));
  };
  sort(root);
  return root;
});
</script>

<template>
  <div class="file-tree">
    <FileTreeNode
      v-for="node in tree"
      :key="node.path"
      :node="node"
      :action="action"
      @select="emit('select', $event)"
    />
  </div>
</template>
