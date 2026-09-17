import i18n from "@/i18n";

export interface AccountInfo {
  _account_id: number;
  username: string;
  name: string;
  email?: string;
  admin?: boolean;
}

export interface SSHKeyInfo {
  id: number;
  public_key: string;
  comment: string;
  created: string;
}

export interface ChangeInfo {
  id: string;
  _number: number;
  project: string;
  branch: string;
  change_id: string;
  subject: string;
  status: "NEW" | "MERGED" | "ABANDONED";
  owner: AccountInfo;
  created: string;
  updated: string;
  submitted?: string;
  topic?: string;
  work_in_progress?: boolean;
  private?: boolean;
  current_revision?: string;
  current_ps?: number;
  submittable?: boolean;
  starred?: boolean;
  submit_type?: string;
  submit_blocked?: string;
  relation_chain?: RelationEntry[];
  revisions?: Record<string, RevisionInfo>;
  labels?: Record<string, LabelInfo>;
  reviewers?: AccountInfo[];
  hashtags?: string[];
  assignee?: AccountInfo;
  attention_set?: AttentionEntry[];
}

export interface AttentionEntry {
  account: AccountInfo;
  reason?: string;
}

export interface RelationEntry {
  _number: number;
  subject: string;
  status: "NEW" | "MERGED" | "ABANDONED";
  relation: "ancestor" | "self" | "descendant";
  self?: boolean;
}

export interface ChangeMessageInfo {
  id: number;
  type: string;
  patch_set: number;
  message: string;
  date: string;
  author?: AccountInfo;
}

export interface RevisionInfo {
  _number: number;
  commit: string;
  created: string;
  author: { name: string; email: string };
  message: string;
}

export interface LabelInfo {
  all: {
    _account_id: number;
    name: string;
    username: string;
    value: number;
    patch_set: number;
  }[];
}

export interface DiffLine {
  type: "context" | "add" | "del";
  text: string;
  old_no?: number;
  new_no?: number;
}

export interface DiffHunk {
  header: string;
  lines: DiffLine[];
}

export interface FileDiff {
  path: string;
  old_path?: string;
  status: "A" | "M" | "D" | "R";
  binary?: boolean;
  hunks: DiffHunk[];
  add_count: number;
  del_count: number;
}

export interface ConflictFile {
  path: string;
  base: string;
  ours: string;
  theirs: string;
  conflict: string;
}

export interface EditInfo {
  commit: string;
  base_commit: string;
  base_ps: number;
  stale: boolean;
}

export interface BlameLine {
  line: number;
  sha: string;
  author: string;
  email: string;
  when: number;
  summary: string;
  text: string;
}

export interface FileLogEntry {
  sha: string;
  author: string;
  email: string;
  when: number;
  subject: string;
}

export interface CommentInfo {
  id: number;
  patch_set: number;
  path: string;
  line: number;
  message: string;
  in_reply_to?: number;
  resolved?: boolean;
  robot_id?: string;
  robot_run_id?: string;
  updated: string;
  author: AccountInfo;
}

export interface CommentDraftInfo {
  id: number;
  patch_set: number;
  path: string;
  line: number;
  message: string;
  in_reply_to?: number;
  updated: string;
}

export interface ProjectInfo {
  name: string;
  description?: string;
}

export const SUBMIT_TYPES = [
  "FAST_FORWARD_ONLY",
  "REBASE_IF_NECESSARY",
  "REBASE_ALWAYS",
  "MERGE_IF_NECESSARY",
  "MERGE_ALWAYS",
  "CHERRY_PICK",
] as const;

export interface SubmitRequirement {
  id?: number;
  project?: string;
  label: string;
  min_value: number;
  block_value: number;
}

export interface ProjectConfig {
  name: string;
  description?: string;
  state?: string;
  submit_type?: string;
  submit_whole_topic?: boolean;
  submit_requirements?: SubmitRequirement[];
}

export interface FileEntry {
  name: string;
  type: "tree" | "blob";
  size?: number;
}

export interface CommitInfo {
  sha: string;
  author: string;
  email: string;
  date: string;
  subject: string;
}

export interface BranchInfo {
  name: string;
  sha: string;
}

export interface TagInfo {
  name: string;
  sha: string;
  message?: string;
}

export interface CheckRun {
  check_name: string;
  state: string;
  url?: string;
  message?: string;
  started?: string;
  finished?: string;
}

export interface Webhook {
  id: number;
  project: string;
  url: string;
  events: string[];
  active: boolean;
  created: string;
}

export interface GroupInfo {
  id: string;
  name: string;
  description?: string;
  system?: boolean;
}

export interface GroupMember {
  _account_id: number;
  username: string;
  name: string;
  email?: string;
}

export interface AccessRuleInfo {
  id?: number;
  project?: string;
  ref_pattern: string;
  permission: string;
  group_id: number;
  group_name?: string;
  action: "ALLOW" | "DENY" | "BLOCK";
  exclusive?: boolean;
  min?: number;
  max?: number;
}

export interface ProjectAccess {
  local: AccessRuleInfo[];
  can_edit: boolean;
}

export interface NotificationInfo {
  id: number;
  change_number: number;
  type: string;
  message: string;
  actor_id?: number;
  read: boolean;
  created: string;
}

export interface NotificationList {
  notifications: NotificationInfo[];
  unread: number;
}

export interface WatchedProjectInfo {
  project: string;
  notify: string;
  branch?: string;
  author?: string;
}

export interface SavedQuery {
  id: number;
  name: string;
  query: string;
  shared: boolean;
  owner?: string;
}

class ApiError extends Error {
  status: number;
  totpRequired?: boolean;
  constructor(status: number, message: string, totpRequired = false) {
    super(message);
    this.status = status;
    this.totpRequired = totpRequired;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    credentials: "include",
    ...init,
    headers: {
      "Accept-Language": i18n.language,
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...init?.headers,
    },
  });
  const text = await res.text();
  // Gerrit XSSI prefix ")]}'"
  const payload = text.startsWith(")]}'") ? text.slice(text.indexOf("\n") + 1) : text;
  let data: unknown = null;
  if (payload.trim()) {
    try {
      data = JSON.parse(payload);
    } catch {
      data = null;
    }
  }
  if (!res.ok) {
    const d = data as { error?: string; totp_required?: boolean } | null;
    const msg = d?.error || `${res.status} ${res.statusText}`;
    throw new ApiError(res.status, msg, !!d?.totp_required);
  }
  return data as T;
}

export const api = {
  login: (username: string, password: string, totp?: string) =>
    request<AccountInfo>("/login", {
      method: "POST",
      body: JSON.stringify({ username, password, ...(totp ? { totp } : {}) }),
    }),
  register: (body: { username: string; password: string; full_name?: string; email?: string }) =>
    request<AccountInfo>("/register", { method: "POST", body: JSON.stringify(body) }),
  logout: () => request<null>("/logout", { method: "POST" }),
  self: () => request<AccountInfo>("/accounts/self"),

  listProjects: () => request<Record<string, ProjectInfo>>("/projects/"),
  createProject: (name: string, description: string, copyFrom?: string) =>
    request<ProjectInfo>("/projects/", {
      method: "POST",
      body: JSON.stringify({ name, description, copy_from: copyFrom || undefined }),
    }),
  branches: (project: string) =>
    request<BranchInfo[]>(`/projects/${encodeURIComponent(project)}/branches`),
  commits: (project: string, revision?: string, n = 50) =>
    request<CommitInfo[]>(
      `/projects/${encodeURIComponent(project)}/commits?n=${n}${revision ? `&revision=${encodeURIComponent(revision)}` : ""}`,
    ),
  commitDiff: (project: string, sha: string) =>
    request<FileDiff[]>(`/projects/${encodeURIComponent(project)}/commitdiff?sha=${encodeURIComponent(sha)}`),
  tree: (project: string, revision?: string, path?: string) =>
    request<FileEntry[]>(
      `/projects/${encodeURIComponent(project)}/tree?revision=${encodeURIComponent(revision ?? "")}&path=${encodeURIComponent(path ?? "")}`,
    ),
  fileText: async (project: string, revision: string, path: string) => {
    const res = await fetch(
      `/projects/${encodeURIComponent(project)}/file?revision=${encodeURIComponent(revision)}&path=${encodeURIComponent(path)}&format=text`,
    );
    if (!res.ok) throw new ApiError(res.status, await res.text());
    return res.text();
  },
  blame: (project: string, revision: string, path: string) =>
    request<BlameLine[]>(
      `/projects/${encodeURIComponent(project)}/blame?revision=${encodeURIComponent(revision)}&path=${encodeURIComponent(path)}`,
    ),
  fileLog: (project: string, revision: string, path: string, n = 50) =>
    request<FileLogEntry[]>(
      `/projects/${encodeURIComponent(project)}/file-log?revision=${encodeURIComponent(revision)}&path=${encodeURIComponent(path)}&n=${n}`,
    ),
  gc: (project: string) =>
    request<{ output: string }>(`/projects/${encodeURIComponent(project)}/gc`, { method: "POST" }),
  fsck: (project: string) =>
    request<{ issues: string[]; healthy: boolean }>(`/projects/${encodeURIComponent(project)}/fsck`),
  projectAccess: (project: string) =>
    request<ProjectAccess>(`/projects/${encodeURIComponent(project)}/access`),
  setProjectAccess: (project: string, rules: AccessRuleInfo[]) =>
    request<ProjectAccess>(`/projects/${encodeURIComponent(project)}/access`, {
      method: "PUT",
      body: JSON.stringify({ rules }),
    }),
  projectConfig: (project: string) =>
    request<ProjectConfig>(`/projects/${encodeURIComponent(project)}`),
  setProjectConfig: (
    project: string,
    body: {
      submit_type?: string;
      submit_whole_topic?: boolean;
      submit_requirements?: SubmitRequirement[];
    },
  ) =>
    request<ProjectConfig>(`/projects/${encodeURIComponent(project)}/config`, {
      method: "PUT",
      body: JSON.stringify(body),
    }),

  tags: (project: string) =>
    request<TagInfo[]>(`/projects/${encodeURIComponent(project)}/tags`),
  createBranch: (project: string, branch: string, revision?: string) =>
    request<{ ref: string; revision: string }>(
      `/projects/${encodeURIComponent(project)}/branches`,
      { method: "POST", body: JSON.stringify({ branch, revision }) },
    ),
  deleteBranch: (project: string, branch: string) =>
    request<null>(
      `/projects/${encodeURIComponent(project)}/branches/${encodeURIComponent(branch)}`,
      { method: "DELETE" },
    ),
  createTag: (project: string, tag: string, revision?: string, message?: string) =>
    request<{ ref: string; revision: string }>(`/projects/${encodeURIComponent(project)}/tags`, {
      method: "POST",
      body: JSON.stringify({ tag, revision, message }),
    }),
  deleteTag: (project: string, tag: string) =>
    request<null>(`/projects/${encodeURIComponent(project)}/tags/${encodeURIComponent(tag)}`, {
      method: "DELETE",
    }),
  editFile: (
    project: string,
    body: { branch: string; path: string; content: string; message?: string },
  ) =>
    request<{ commit: string; branch: string; path: string }>(
      `/projects/${encodeURIComponent(project)}/edit`,
      { method: "PUT", body: JSON.stringify(body) },
    ),
  setProjectState: (project: string, state: string) =>
    request<{ name: string; state: string }>(
      `/projects/${encodeURIComponent(project)}/state`,
      { method: "PUT", body: JSON.stringify({ state }) },
    ),
  deleteProject: (project: string) =>
    request<null>(`/projects/${encodeURIComponent(project)}`, { method: "DELETE" }),

  listGroups: () => request<Record<string, GroupInfo>>("/groups/"),
  createGroup: (name: string, description = "") =>
    request<GroupInfo>("/groups/", { method: "POST", body: JSON.stringify({ name, description }) }),
  deleteGroup: (id: string) => request<null>(`/groups/${encodeURIComponent(id)}`, { method: "DELETE" }),
  groupMembers: (id: string) =>
    request<Record<string, GroupMember>>(`/groups/${encodeURIComponent(id)}/members`),
  addGroupMember: (id: string, account: string) =>
    request<GroupMember>(`/groups/${encodeURIComponent(id)}/members/${encodeURIComponent(account)}`, {
      method: "PUT",
    }),
  removeGroupMember: (id: string, account: string) =>
    request<null>(`/groups/${encodeURIComponent(id)}/members/${encodeURIComponent(account)}`, {
      method: "DELETE",
    }),

  listChanges: (q = "") => request<ChangeInfo[]>(`/changes/?q=${encodeURIComponent(q)}`),
  listChangesPaged: async (
    q = "",
    n = 50,
    start = 0,
  ): Promise<{ items: ChangeInfo[]; total: number }> => {
    const params = new URLSearchParams();
    if (q) params.set("q", q);
    params.set("n", String(n));
    if (start > 0) params.set("start", String(start));
    const res = await fetch(`/changes/?${params.toString()}`, {
      credentials: "include",
      headers: { "Accept-Language": i18n.language },
    });
    const text = await res.text();
    const payload = text.startsWith(")]}'") ? text.slice(text.indexOf("\n") + 1) : text;
    if (!res.ok) {
      let msg = `${res.status} ${res.statusText}`;
      try {
        msg = (JSON.parse(payload) as { error?: string }).error || msg;
      } catch {
        /* ignore */
      }
      throw new ApiError(res.status, msg);
    }
    const total = Number(res.headers.get("X-Total-Count") ?? "0");
    const items = payload.trim() ? (JSON.parse(payload) as ChangeInfo[]) : [];
    return { items, total };
  },
  changeDetail: (num: number | string) => request<ChangeInfo>(`/changes/${num}`),
  revisions: (num: number | string) => request<RevisionInfo[]>(`/changes/${num}/revisions`),
  revisionFiles: (num: number | string, ps: number | "current" = "current") =>
    request<FileDiff[]>(`/changes/${num}/revisions/${ps}/files`),
  revisionPatch: async (num: number | string, ps: number | "current" = "current") => {
    const b64 = await request<string>(`/changes/${num}/revisions/${ps}/patch`);
    return atob(b64);
  },
  revisionFileContent: async (num: number | string, ps: number | "current", path: string) => {
    const res = await fetch(
      `/changes/${num}/revisions/${ps}/file?path=${encodeURIComponent(path)}&format=text`,
    );
    if (!res.ok) throw new ApiError(res.status, await res.text());
    return res.text();
  },
  comments: (num: number | string) => request<CommentInfo[]>(`/changes/${num}/comments`),
  resolveComment: (num: number | string, id: number, resolved: boolean) =>
    request<{ id: number; resolved: boolean }>(`/changes/${num}/comments/${id}/resolve`, {
      method: "PUT",
      body: JSON.stringify({ resolved }),
    }),
  listDrafts: (num: number | string) =>
    request<CommentDraftInfo[]>(`/changes/${num}/drafts`),
  putDraft: (
    num: number | string,
    body: { id?: number; path: string; line: number; message: string; in_reply_to?: number },
  ) => request<CommentDraftInfo>(`/changes/${num}/drafts`, { method: "PUT", body: JSON.stringify(body) }),
  deleteDraft: (num: number | string, id: number) =>
    request<null>(`/changes/${num}/drafts/${id}`, { method: "DELETE" }),
  messages: (num: number | string) =>
    request<ChangeMessageInfo[]>(`/changes/${num}/messages`),
  addReviewer: (num: number | string, reviewer: string) =>
    request<AccountInfo[]>(`/changes/${num}/reviewers`, {
      method: "POST",
      body: JSON.stringify({ reviewer }),
    }),
  removeReviewer: (num: number | string, id: number) =>
    request<AccountInfo[]>(`/changes/${num}/reviewers/${id}`, { method: "DELETE" }),
  suggestReviewers: (num: number | string) =>
    request<AccountInfo[]>(`/changes/${num}/suggest-reviewers`),
  setTopic: (num: number | string, topic: string) =>
    request<{ topic: string }>(`/changes/${num}/topic`, {
      method: "PUT",
      body: JSON.stringify({ topic }),
    }),
  deleteTopic: (num: number | string) =>
    request<null>(`/changes/${num}/topic`, { method: "DELETE" }),
  setWIP: (num: number | string) =>
    request<{ work_in_progress: boolean }>(`/changes/${num}/wip`, { method: "PUT" }),
  clearWIP: (num: number | string) =>
    request<{ work_in_progress: boolean }>(`/changes/${num}/wip`, { method: "DELETE" }),
  review: (
    num: number | string,
    body: {
      labels?: Record<string, number>;
      message?: string;
      comments?: Record<string, { line: number; message: string; in_reply_to?: number }[]>;
    },
  ) => request<{ ok: boolean }>(`/changes/${num}/review`, { method: "POST", body: JSON.stringify(body) }),
  submit: (num: number | string) =>
    request<{ status: string }>(`/changes/${num}/submit`, { method: "POST" }),
  rebase: (num: number | string) =>
    request<ChangeInfo>(`/changes/${num}/rebase`, { method: "POST" }),
  rebaseConflicts: (num: number | string) =>
    request<ConflictFile[]>(`/changes/${num}/rebase/conflicts`),
  resolveRebase: (num: number | string, resolutions: Record<string, string>) =>
    request<ChangeInfo>(`/changes/${num}/rebase/resolve`, {
      method: "POST",
      body: JSON.stringify({ resolutions }),
    }),
  getEdit: (num: number | string) => request<EditInfo>(`/changes/${num}/edit`),
  createEdit: (num: number | string) =>
    request<EditInfo>(`/changes/${num}/edit`, { method: "PUT" }),
  deleteEdit: (num: number | string) =>
    request<null>(`/changes/${num}/edit`, { method: "DELETE" }),
  putEditFile: (num: number | string, path: string, content: string) =>
    request<EditInfo>(`/changes/${num}/edit/file`, {
      method: "PUT",
      body: JSON.stringify({ path, content }),
    }),
  deleteEditFile: (num: number | string, path: string) =>
    request<EditInfo>(`/changes/${num}/edit/file?path=${encodeURIComponent(path)}`, { method: "DELETE" }),
  publishEdit: (num: number | string) =>
    request<ChangeInfo>(`/changes/${num}/edit:publish`, { method: "POST" }),
  rebaseEdit: (num: number | string) =>
    request<EditInfo>(`/changes/${num}/edit:rebase`, { method: "POST" }),
  cherryPick: (num: number | string, destination: string) =>
    request<ChangeInfo>(`/changes/${num}/cherry_pick`, {
      method: "POST",
      body: JSON.stringify({ destination }),
    }),
  revert: (num: number | string) =>
    request<ChangeInfo>(`/changes/${num}/revert`, { method: "POST" }),
  abandon: (num: number | string) =>
    request<{ status: string }>(`/changes/${num}/abandon`, { method: "POST" }),
  restore: (num: number | string) =>
    request<{ status: string }>(`/changes/${num}/restore`, { method: "POST" }),
  star: (num: number | string) =>
    request<{ starred: boolean }>(`/changes/${num}/star`, { method: "PUT" }),
  unstar: (num: number | string) =>
    request<{ starred: boolean }>(`/changes/${num}/star`, { method: "DELETE" }),

  hashtags: (num: number | string) => request<string[]>(`/changes/${num}/hashtags`),
  setHashtags: (num: number | string, add: string[] = [], remove: string[] = []) =>
    request<string[]>(`/changes/${num}/hashtags`, {
      method: "PUT",
      body: JSON.stringify({ add, remove }),
    }),
  assignee: (num: number | string) => request<AccountInfo | null>(`/changes/${num}/assignee`),
  setAssignee: (num: number | string, assignee: string) =>
    request<AccountInfo>(`/changes/${num}/assignee`, {
      method: "PUT",
      body: JSON.stringify({ assignee }),
    }),
  deleteAssignee: (num: number | string) =>
    request<null>(`/changes/${num}/assignee`, { method: "DELETE" }),
  attention: (num: number | string) =>
    request<AttentionEntry[]>(`/changes/${num}/attention`),
  addAttention: (num: number | string, user: string, reason?: string) =>
    request<AttentionEntry[]>(`/changes/${num}/attention`, {
      method: "PUT",
      body: JSON.stringify({ user, reason }),
    }),
  removeAttention: (num: number | string, id: number) =>
    request<AttentionEntry[]>(`/changes/${num}/attention/${id}`, { method: "DELETE" }),

  listCheckRuns: (num: number | string, ps: number | "current" = "current") =>
    request<CheckRun[]>(`/changes/${num}/revisions/${ps}/checkruns`),
  upsertCheckRun: (
    num: number | string,
    ps: number | "current",
    body: { check_name: string; state: string; url?: string; message?: string },
  ) =>
    request<CheckRun>(`/changes/${num}/revisions/${ps}/checkruns`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  deleteCheckRun: (num: number | string, ps: number | "current", name: string) =>
    request<null>(
      `/changes/${num}/revisions/${ps}/checkruns/${encodeURIComponent(name)}`,
      { method: "DELETE" },
    ),

  listWebhooks: (project: string) =>
    request<Webhook[]>(`/projects/${encodeURIComponent(project)}/webhooks`),
  createWebhook: (
    project: string,
    body: { url: string; events?: string[]; secret?: string; active?: boolean },
  ) =>
    request<Webhook>(`/projects/${encodeURIComponent(project)}/webhooks`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  deleteWebhook: (project: string, id: number) =>
    request<null>(`/projects/${encodeURIComponent(project)}/webhooks/${id}`, {
      method: "DELETE",
    }),
  listGlobalWebhooks: () => request<Webhook[]>("/config/webhooks"),
  createGlobalWebhook: (body: {
    url: string;
    events?: string[];
    secret?: string;
    active?: boolean;
  }) => request<Webhook>("/config/webhooks", { method: "POST", body: JSON.stringify(body) }),
  deleteGlobalWebhook: (id: number) =>
    request<null>(`/config/webhooks/${id}`, { method: "DELETE" }),

  watchProject: (project: string, notify = "ALL", branch = "", author = "") =>
    request<{ project: string; notify: string; watched: boolean }>(
      `/projects/${encodeURIComponent(project)}/watch`,
      { method: "PUT", body: JSON.stringify({ notify, branch, author }) },
    ),
  unwatchProject: (project: string) =>
    request<{ project: string; watched: boolean }>(
      `/projects/${encodeURIComponent(project)}/watch`,
      { method: "DELETE" },
    ),
  listWatched: () => request<WatchedProjectInfo[]>("/accounts/self/watched"),
  listSavedQueries: () => request<SavedQuery[]>("/accounts/self/queries"),
  createSavedQuery: (name: string, query: string, shared: boolean) =>
    request<SavedQuery>("/accounts/self/queries", {
      method: "POST",
      body: JSON.stringify({ name, query, shared }),
    }),
  deleteSavedQuery: (id: number) =>
    request<null>(`/accounts/self/queries/${id}`, { method: "DELETE" }),

  listNotifications: (n = 50, unreadOnly = false) =>
    request<NotificationList>(
      `/accounts/self/notifications?n=${n}${unreadOnly ? "&unread=1" : ""}`,
    ),
  markNotificationsRead: (id?: number) =>
    request<{ unread: number }>("/accounts/self/notifications/read", {
      method: "POST",
      body: JSON.stringify(id ? { id } : {}),
    }),

  getConfig: () => request<{ auth: { oauth: boolean; ldap?: boolean; register?: boolean }; ssh?: { port?: string } }>("/config"),
  updateSelf: (body: { name?: string; email?: string }) =>
    request<AccountInfo>("/accounts/self", { method: "PUT", body: JSON.stringify(body) }),
  setPassword: (oldPassword: string, newPassword: string) =>
    request<null>("/accounts/self/password", {
      method: "PUT",
      body: JSON.stringify({ old_password: oldPassword, new_password: newPassword }),
    }),
  listAccounts: () => request<AccountInfo[]>("/accounts/"),
  createAccount: (body: { username: string; password: string; name?: string; email?: string }) =>
    request<AccountInfo>("/accounts/", { method: "POST", body: JSON.stringify(body) }),
  httpPasswordStatus: () =>
    request<{ enabled: boolean }>("/accounts/self/http-password"),
  generateHTTPPassword: () =>
    request<{ http_password: string }>("/accounts/self/http-password", { method: "PUT" }),
  clearHTTPPassword: () =>
    request<null>("/accounts/self/http-password", { method: "DELETE" }),
  listSSHKeys: () => request<SSHKeyInfo[]>("/accounts/self/sshkeys"),
  addSSHKey: (key: string) =>
    request<SSHKeyInfo>("/accounts/self/sshkeys", {
      method: "POST",
      body: JSON.stringify({ key }),
    }),
  deleteSSHKey: (id: number) =>
    request<null>(`/accounts/self/sshkeys/${id}`, { method: "DELETE" }),

  get2FA: () => request<{ enabled: boolean; enrolled: boolean }>("/accounts/self/2fa"),
  enroll2FA: () =>
    request<{ secret: string; otpauth_uri: string }>("/accounts/self/2fa/enroll", {
      method: "POST",
    }),
  enable2FA: (code: string) =>
    request<null>("/accounts/self/2fa/enable", {
      method: "POST",
      body: JSON.stringify({ code }),
    }),
  disable2FA: (code: string) =>
    request<null>("/accounts/self/2fa/disable", {
      method: "POST",
      body: JSON.stringify({ code }),
    }),
};

export { ApiError };
