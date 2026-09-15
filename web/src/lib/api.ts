export interface AccountInfo {
  _account_id: number;
  username: string;
  name: string;
  email?: string;
  admin?: boolean;
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
  current_revision?: string;
  current_ps?: number;
  submittable?: boolean;
  revisions?: Record<string, RevisionInfo>;
  labels?: Record<string, LabelInfo>;
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

export interface CommentInfo {
  id: number;
  patch_set: number;
  path: string;
  line: number;
  message: string;
  in_reply_to?: number;
  updated: string;
  author: AccountInfo;
}

export interface ProjectInfo {
  name: string;
  description?: string;
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

class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    credentials: "include",
    ...init,
    headers: {
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
    const msg =
      (data as { error?: string } | null)?.error || `${res.status} ${res.statusText}`;
    throw new ApiError(res.status, msg);
  }
  return data as T;
}

export const api = {
  login: (username: string, password: string) =>
    request<AccountInfo>("/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),
  register: (body: { username: string; password: string; full_name?: string; email?: string }) =>
    request<AccountInfo>("/register", { method: "POST", body: JSON.stringify(body) }),
  logout: () => request<null>("/logout", { method: "POST" }),
  self: () => request<AccountInfo>("/accounts/self"),

  listProjects: () => request<Record<string, ProjectInfo>>("/projects/"),
  createProject: (name: string, description: string) =>
    request<ProjectInfo>("/projects/", {
      method: "POST",
      body: JSON.stringify({ name, description }),
    }),
  branches: (project: string) =>
    request<BranchInfo[]>(`/projects/${encodeURIComponent(project)}/branches`),
  commits: (project: string, revision?: string, n = 50) =>
    request<CommitInfo[]>(
      `/projects/${encodeURIComponent(project)}/commits?n=${n}${revision ? `&revision=${encodeURIComponent(revision)}` : ""}`,
    ),
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

  listChanges: (q = "") => request<ChangeInfo[]>(`/changes/?q=${encodeURIComponent(q)}`),
  changeDetail: (num: number | string) => request<ChangeInfo>(`/changes/${num}`),
  revisions: (num: number | string) => request<RevisionInfo[]>(`/changes/${num}/revisions`),
  revisionFiles: (num: number | string, ps: number | "current" = "current") =>
    request<FileDiff[]>(`/changes/${num}/revisions/${ps}/files`),
  revisionPatch: async (num: number | string, ps: number | "current" = "current") => {
    const b64 = await request<string>(`/changes/${num}/revisions/${ps}/patch`);
    return atob(b64);
  },
  comments: (num: number | string) => request<CommentInfo[]>(`/changes/${num}/comments`),
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
  abandon: (num: number | string) =>
    request<{ status: string }>(`/changes/${num}/abandon`, { method: "POST" }),
  restore: (num: number | string) =>
    request<{ status: string }>(`/changes/${num}/restore`, { method: "POST" }),
};

export { ApiError };
