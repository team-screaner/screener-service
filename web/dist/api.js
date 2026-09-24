export class ApiError extends Error {
  constructor(message, status = 0) {
    super(message);
    this.status = status;
  }
}

export class Api {
  constructor({
    fetch = globalThis.fetch.bind(globalThis),
    storage = globalThis.sessionStorage,
    onUnauthorized = () => {},
  } = {}) {
    this.fetch = fetch;
    this.storage = storage;
    this.onUnauthorized = onUnauthorized;
    this.pending = new Map();
    try {
      this.token = storage?.getItem("screener.session") || "";
    } catch {
      this.token = "";
    }
  }
  setToken(token) {
    this.token = token;
    this.pending.clear();
    try {
      if (token) this.storage?.setItem("screener.session", token);
      else this.storage?.removeItem("screener.session");
    } catch {
      /* An in-memory session remains usable when storage is disabled. */
    }
  }
  async request(path, { method = "GET", body, key, binary = false } = {}) {
    const headers = {
      Accept: binary
        ? "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
        : "application/json",
    };
    if (this.token) headers.Authorization = `Bearer ${this.token}`;
    if (body !== undefined) headers["Content-Type"] = "application/json";
    if (key) headers["Idempotency-Key"] = key;
    let res;
    try {
      res = await this.fetch(`/api/v1${path}`, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
        signal: AbortSignal.timeout(35000),
      });
    } catch {
      throw new ApiError(
        "Не удалось связаться с сервером. Проверьте соединение и повторите запрос.",
      );
    }
    if (res.status === 401) {
      const hadSession = !!this.token;
      this.setToken("");
      if (hadSession) this.onUnauthorized();
      throw new ApiError(
        hadSession
          ? "Сессия завершилась. Войдите снова."
          : "Проверьте почту и пароль.",
        401,
      );
    }
    if (binary && res.ok) return res.blob();
    const data = await res.json().catch(() => null);
    if (!res.ok)
      throw new ApiError(
        data?.message || `Сервер вернул ошибку ${res.status}.`,
        res.status,
      );
    return data;
  }
  get(path, query = {}) {
    const params = new URLSearchParams(
      Object.entries(query).filter(
        ([, value]) => value !== "" && value != null,
      ),
    );
    return this.request(`${path}${params.size ? `?${params}` : ""}`);
  }
  async all(path, query = {}) {
    const items = [],
      seen = new Set();
    let cursor = "";
    do {
      const page = await this.get(path, { ...query, limit: 100, cursor });
      items.push(...page.items);
      cursor = page.next_cursor || "";
      if (cursor && seen.has(cursor))
        throw new ApiError("Не удалось загрузить следующую страницу.");
      seen.add(cursor);
    } while (cursor);
    return items;
  }
  async command(path, body, method = "POST") {
    const identity = JSON.stringify([method, path, body]);
    const key = this.pending.get(identity) || crypto.randomUUID();
    this.pending.set(identity, key);
    try {
      const result = await this.request(path, { method, body, key });
      this.pending.delete(identity);
      return result;
    } catch (error) {
      if (error.status >= 400 && error.status < 500)
        this.pending.delete(identity);
      throw error;
    }
  }
}
