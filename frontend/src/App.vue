<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  LayoutDashboard,
  FolderKanban,
  Workflow,
  Activity,
  Server,
  Variable,
  Bot,
  PanelLeftClose,
  PanelLeftOpen,
  Command,
  LogOut,
  X,
  Search,
  UserRound,
  Users,
} from "lucide-vue-next";
import { api,session } from "./api";
const route = useRoute(),
  router = useRouter(),
  collapsed = ref(localStorage.getItem("sidebar-collapsed") === "1"),
  mobile = ref(false),
  commands = ref(false),
  query = ref(""),me=ref<any>();
const login = computed(() => route.path === "/login");
const items = computed(()=>[
  ["概览", "/", LayoutDashboard],
  ["项目", "/projects", FolderKanban],
  ["工作流", "/workflows", Workflow],
  ["运行记录", "/runs", Activity],
  ["SSH 资源", "/settings/ssh", Server],
  ["环境变量", "/settings/environment", Variable],
  ["AI 模型", "/settings/ai", Bot],
  ["个人设置","/settings/account",UserRound],
  ...(me.value?.is_admin?[["用户管理","/admin/users",Users]]:[]),
]);
function toggle() {
  collapsed.value = !collapsed.value;
  localStorage.setItem("sidebar-collapsed", collapsed.value ? "1" : "0");
}
function logout() {
  session.token = "";
  router.push("/login");
}
function inputFocused() {
  const t = document.activeElement?.tagName;
  return t === "INPUT" || t === "TEXTAREA" || t === "SELECT";
}
function key(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
    e.preventDefault();
    commands.value = true;
  } else if (e.key === "?" && !inputFocused()) commands.value = true;
}
onMounted(() => {document.addEventListener("keydown", key);if(session.token)api('/api/auth/me').then(x=>me.value=x)});
onUnmounted(() => document.removeEventListener("keydown", key));
function go(path: string) {
  commands.value = false;
  query.value = "";
  router.push(path);
}
</script>
<template>
  <router-view v-if="login" />
  <div v-else class="app-shell" :class="{ collapsed }">
    <div v-if="mobile" class="mobile-shade" @click="mobile = false"></div>
    <aside class="sidebar" :class="{ mobile }">
      <div class="logo">
        <span class="logo-mark">P</span><b>Product Server</b
        ><button class="icon-btn desktop-only" @click="toggle">
          <PanelLeftOpen v-if="collapsed" /><PanelLeftClose v-else /></button
        ><button class="icon-btn mobile-only" @click="mobile = false">
          <X />
        </button>
      </div>
      <nav>
        <router-link
          v-for="[label, path, icon] in items"
          :key="label as string"
          :to="path as string"
          :title="label as string"
          ><component :is="icon" :size="19" /><span>{{
            label
          }}</span></router-link
        >
      </nav>
      <div class="sidebar-foot">
        <button class="side-action" @click="commands = true">
          <Command :size="18" /><span>快捷命令</span><kbd>⌘K</kbd></button
        ><button class="side-action" @click="logout">
          <LogOut :size="18" /><span>退出登录</span>
        </button>
      </div>
    </aside>
    <div class="app-main">
      <header class="topbar">
        <button class="icon-btn mobile-only" @click="mobile = true">
          <PanelLeftOpen />
        </button>
        <div class="crumb">{{ route.meta.title || "Product Server" }}</div>
        <button class="search-trigger" @click="commands = true">
          <Search :size="16" />搜索或执行命令 <kbd>⌘K</kbd>
        </button>
      </header>
      <main><router-view /></main>
    </div>
  </div>
  <Teleport to="body"
    ><div
      v-if="commands"
      class="overlay command-overlay"
      @mousedown.self="commands = false"
    >
      <div class="command-box">
        <div class="command-search">
          <Search /><input
            v-model="query"
            autofocus
            placeholder="输入页面名称…"
          />
        </div>
        <button
          v-for="[label, path, icon] in items.filter((x) =>
            String(x[0]).includes(query),
          )"
          :key="label as string"
          @click="go(path as string)"
        >
          <component :is="icon" /><span>前往{{ label }}</span>
        </button>
      </div>
    </div></Teleport
  >
</template>
