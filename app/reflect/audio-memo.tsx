"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Mic, Play, RefreshCw, Square, X } from "lucide-react";
import type { AiProviderConfig } from "./ai";

interface AudioMemo {
  id: string;
  notebookId: string;
  recordedDate: string;
  fileName: string;
  mimeType: string;
  status: "pending" | "transcribing" | "failed" | "blocked" | "done";
  error?: string;
  transcriptDocumentId?: string;
  createdAt: string;
}

interface AudioMemoPanelProps {
  open: boolean;
  notebookId: string;
  providers: readonly AiProviderConfig[];
  onClose: () => void;
  onOpenDocument: (documentId: string) => void;
  onSaved: () => void;
  onNotice: (message: string) => void;
}

export function AudioMemoPanel({ open, notebookId, providers, onClose, onOpenDocument, onSaved, onNotice }: AudioMemoPanelProps) {
  const [memos, setMemos] = useState<AudioMemo[]>([]);
  const [recording, setRecording] = useState(false);
  const [elapsed, setElapsed] = useState(0);
  const [levels, setLevels] = useState<number[]>(Array(24).fill(0.12));
  const recorderRef = useRef<MediaRecorder | null>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const chunksRef = useRef<Blob[]>([]);
  const startedRef = useRef(0);
  const animationRef = useRef(0);
  const timerRef = useRef(0);
  const audioContextRef = useRef<AudioContext | null>(null);

  const load = useCallback(async () => {
    const response = await fetch(`/api/audio-memos?notebookId=${encodeURIComponent(notebookId)}`);
    if (response.ok) setMemos((await response.json()) as AudioMemo[]);
  }, [notebookId]);

  useEffect(() => {
    if (open) queueMicrotask(() => void load());
  }, [load, open]);

  const stopTracks = useCallback(() => {
    window.cancelAnimationFrame(animationRef.current);
    window.clearInterval(timerRef.current);
    streamRef.current?.getTracks().forEach((track) => track.stop());
    streamRef.current = null;
    void audioContextRef.current?.close();
    audioContextRef.current = null;
  }, []);

  useEffect(() => stopTracks, [stopTracks]);

  const transcriptionProvider =
    providers.find((provider) => provider.isDefault && ["openai", "google"].includes(provider.provider)) ??
    providers.find((provider) => ["openai", "google"].includes(provider.provider));

  const transcribe = useCallback(async (memo: AudioMemo) => {
    if (!transcriptionProvider) return;
    setMemos((current) => current.map((item) => item.id === memo.id ? { ...item, status: "transcribing", error: "" } : item));
    const response = await fetch(`/api/audio-memos/${encodeURIComponent(memo.id)}/transcribe`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ providerId: transcriptionProvider.id }),
    });
    if (!response.ok) {
      const message = await response.text();
      setMemos((current) => current.map((item) => item.id === memo.id ? { ...item, status: response.status === 403 ? "blocked" : "failed", error: message } : item));
      onNotice(message);
      return;
    }
    const updated = (await response.json()) as AudioMemo;
    setMemos((current) => current.map((item) => item.id === memo.id ? updated : item));
    onSaved();
  }, [onNotice, onSaved, transcriptionProvider]);

  const persistRecording = useCallback(async (blob: Blob) => {
    const form = new FormData();
    form.append("notebookId", notebookId);
    form.append("audio", blob, `memo.${blob.type.includes("ogg") ? "ogg" : "webm"}`);
    const response = await fetch("/api/audio-memos", { method: "POST", body: form });
    if (!response.ok) {
      onNotice(await response.text());
      return;
    }
    const memo = (await response.json()) as AudioMemo;
    setMemos((current) => [memo, ...current]);
    onSaved();
    if (transcriptionProvider) void transcribe(memo);
    else onNotice("录音已保存；添加 OpenAI 或 Google 后可转写");
  }, [notebookId, onNotice, onSaved, transcribe, transcriptionProvider]);

  const start = async () => {
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      streamRef.current = stream;
      const preferred = ["audio/webm;codecs=opus", "audio/ogg;codecs=opus"].find((type) => MediaRecorder.isTypeSupported(type));
      const recorder = new MediaRecorder(stream, preferred ? { mimeType: preferred } : undefined);
      recorderRef.current = recorder;
      chunksRef.current = [];
      recorder.ondataavailable = (event) => {
        if (event.data.size > 0) chunksRef.current.push(event.data);
      };
      recorder.onstop = () => {
        const blob = new Blob(chunksRef.current, { type: recorder.mimeType || "audio/webm" });
        stopTracks();
        setRecording(false);
        void persistRecording(blob);
      };
      recorder.start(1000);
      startedRef.current = Date.now();
      setElapsed(0);
      setRecording(true);
      timerRef.current = window.setInterval(() => setElapsed(Date.now() - startedRef.current), 250);
      const context = new AudioContext();
      audioContextRef.current = context;
      const analyser = context.createAnalyser();
      analyser.fftSize = 64;
      context.createMediaStreamSource(stream).connect(analyser);
      const data = new Uint8Array(analyser.frequencyBinCount);
      const draw = () => {
        analyser.getByteFrequencyData(data);
        setLevels(Array.from(data.slice(0, 24), (value) => Math.max(0.12, value / 255)));
        animationRef.current = window.requestAnimationFrame(draw);
      };
      draw();
    } catch {
      onNotice("无法访问麦克风，请检查浏览器权限");
    }
  };

  const stop = () => recorderRef.current?.stop();
  const formatTime = (milliseconds: number) => `${String(Math.floor(milliseconds / 60000)).padStart(2, "0")}:${String(Math.floor(milliseconds / 1000) % 60).padStart(2, "0")}`;

  if (!open) return null;
  return (
    <div className="audio-panel-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget && !recording) onClose(); }}>
      <aside className="audio-panel">
        <header><div><Mic size={17} /><strong>音频备忘</strong></div><button type="button" onClick={onClose} disabled={recording}><X size={17} /></button></header>
        <section className="audio-recorder-card">
          <div className={`audio-waveform ${recording ? "live" : ""}`}>{levels.map((level, index) => <i key={index} style={{ height: `${Math.round(8 + level * 38)}px` }} />)}</div>
          <strong>{recording ? formatTime(elapsed) : "随时记录一个想法"}</strong>
          <button type="button" className={recording ? "record-stop" : "record-start"} onClick={recording ? stop : () => void start()}>{recording ? <Square size={18} /> : <Mic size={20} />}</button>
          <small>录音先保存在本机，再使用你的 BYOK 供应商异步转写。</small>
        </section>
        <section className="audio-memo-list">
          <h3>最近录音</h3>
          {memos.map((memo) => (
            <div className="audio-memo-row" key={memo.id}>
              <audio controls preload="none" src={`/audio/${memo.fileName}`} />
              <span><strong>{new Date(memo.createdAt).toLocaleString("zh-CN", { dateStyle: "short", timeStyle: "short" })}</strong><small>{memo.status === "done" ? "已转写" : memo.status === "transcribing" ? "正在转写…" : memo.status === "blocked" ? "私密每日笔记：未发送" : memo.status === "failed" ? memo.error : "等待转写"}</small></span>
              {memo.transcriptDocumentId ? <button type="button" onClick={() => onOpenDocument(memo.transcriptDocumentId!)}><Play size={14} /> 打开</button> : <button type="button" onClick={() => void transcribe(memo)} disabled={!transcriptionProvider || memo.status === "transcribing"}><RefreshCw size={14} /> 转写</button>}
            </div>
          ))}
        </section>
      </aside>
    </div>
  );
}
