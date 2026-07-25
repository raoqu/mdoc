"use client";

import { useEffect } from "react";

export function PwaRegistration() {
  useEffect(() => {
    if (!("serviceWorker" in navigator)) return;

    // Vinext snapshots public/ routes when the dev server starts. Registering a
    // worker during HMR can therefore request a newly-added sw.js before that
    // route exists, and browsers surface the rejected promise as an overlay.
    // A worker is only useful for the production build; remove stale workers
    // in development so cached app shells cannot interfere with local edits.
    if (process.env.NODE_ENV !== "production") {
      void navigator.serviceWorker
        .getRegistrations()
        .then((registrations) =>
          Promise.all(registrations.map((registration) => registration.unregister())),
        )
        .catch(() => undefined);
      return;
    }

    const register = () => {
      void navigator.serviceWorker
        .register("/sw.js", { scope: "/" })
        .catch(() => undefined);
    };
    if (document.readyState === "complete") register();
    else window.addEventListener("load", register, { once: true });
    return () => window.removeEventListener("load", register);
  }, []);
  return null;
}
