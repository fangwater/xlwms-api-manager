import { useEffect, useState } from "react";

const base = import.meta.env.BASE_URL.replace(/\/$/, "");

function currentPath(): string {
  const path = window.location.pathname.startsWith(base) ? window.location.pathname.slice(base.length) : window.location.pathname;
  return path || "/";
}

export function useRouter() {
  const [path, setPath] = useState(currentPath);
  useEffect(() => {
    const update = () => setPath(currentPath());
    window.addEventListener("popstate", update);
    return () => window.removeEventListener("popstate", update);
  }, []);
  const navigate = (next: string) => {
    const normalized = next.startsWith("/") ? next : `/${next}`;
    window.history.pushState({}, "", `${base}${normalized}`);
    window.dispatchEvent(new PopStateEvent("popstate"));
  };
  return { path, navigate };
}
