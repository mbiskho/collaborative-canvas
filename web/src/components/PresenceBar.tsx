import type { ProviderState } from "../lib/provider";
import type { Peer } from "../hooks/useCanvas";

const STATUS: Record<ProviderState["status"], { label: string; dot: string }> = {
  connecting: { label: "Connecting", dot: "bg-amber-400" },
  syncing: { label: "Syncing", dot: "bg-amber-400" },
  synced: { label: "Live", dot: "bg-emerald-500" },
  offline: { label: "Offline: edits saved locally", dot: "bg-red-500" },
};

export function PresenceBar({ peers, conn, room }: { peers: Peer[]; conn: ProviderState; room: string }) {
  const s = STATUS[conn.status];
  return (
    <div className="flex items-center gap-3">
      <div className="flex -space-x-2">
        {peers.slice(0, 6).map((p) => (
          <div
            key={p.clientId}
            title={p.name}
            className="flex h-8 w-8 items-center justify-center rounded-full border-2 border-white text-xs font-semibold text-white"
            style={{ background: p.color }}
          >
            {p.name[0]}
          </div>
        ))}
        {peers.length > 6 && (
          <div className="flex h-8 w-8 items-center justify-center rounded-full border-2 border-white bg-neutral-300 text-xs">
            +{peers.length - 6}
          </div>
        )}
      </div>
      <div className="flex items-center gap-2 rounded-full border border-neutral-200 bg-white px-3 py-1 text-xs text-neutral-600">
        <span className={`h-2 w-2 rounded-full ${s.dot}`} />
        {s.label}
        {conn.pendingOffline > 0 && <span className="text-neutral-400">· {conn.pendingOffline} queued</span>}
      </div>
      <span className="text-xs text-neutral-400">room {room}</span>
    </div>
  );
}
