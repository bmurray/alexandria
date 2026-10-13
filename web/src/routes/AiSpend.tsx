import { useCallback, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, errorMessage } from '../api/client'
import type { AiProvider, ProviderPatch } from '../schemas/ai'
import { useToast } from '../hooks/useToast'
import { formatInt, formatUsd } from '../lib/format'
import { Empty, ErrorState, Loading } from '../components/States'

export function AiSpend() {
  const usage = useQuery({ queryKey: ['ai', 'usage'] as const, queryFn: api.aiUsage })
  const qc = useQueryClient()
  const toast = useToast()
  const patch = useMutation({
    mutationFn: ({ name, patch }: { name: string; patch: ProviderPatch }) => api.patchProvider(name, patch),
    onSuccess: async (res) => {
      toast.push('success', `Updated ${res.provider.name}`)
      await qc.invalidateQueries({ queryKey: ['ai', 'usage'] })
    },
    onError: (err) => toast.push('error', errorMessage(err, 'Could not update the provider')),
  })

  if (usage.isPending) return <Loading label="Adding up the bill…" />
  if (usage.isError) return <ErrorState error={usage.error} onRetry={() => void usage.refetch()} />
  const d = usage.data
  const providers = [...(d.providers ?? [])].sort((a, b) => (a.priority ?? 0) - (b.priority ?? 0))

  return (
    <section className="page-section">
      <h1 className="display">AI spend</h1>
      <div className="tiles tiles--3" data-testid="ai-totals">
        <div className="tile">
          <span className="tile__value">{formatUsd(d.totals?.todayUsd)}</span>
          <span className="tile__label">Today</span>
        </div>
        <div className="tile">
          <span className="tile__value">{formatUsd(d.totals?.allTimeUsd)}</span>
          <span className="tile__label">All time</span>
        </div>
        <div className="tile">
          <span className="tile__value">{formatInt(d.totals?.calls)}</span>
          <span className="tile__label">Model calls</span>
        </div>
      </div>

      <h2>Providers</h2>
      <p className="muted">The lowest priority number that is enabled and under its cap answers first; the rest are fallbacks.</p>
      {providers.length === 0 ? (
        <Empty title="No AI providers are configured" />
      ) : (
        <div className="table-wrap">
          <table className="table" data-testid="providers-table">
            <thead>
              <tr>
                <th scope="col">Provider</th>
                <th scope="col">Model</th>
                <th scope="col">Enabled</th>
                <th scope="col">Priority</th>
                <th scope="col">Spend limit (USD)</th>
                <th scope="col">Spent</th>
              </tr>
            </thead>
            <tbody>
              {providers.map((p) => (
                <ProviderRow key={p.name} provider={p} busy={patch.isPending} onPatch={(body) => patch.mutate({ name: p.name, patch: body })} />
              ))}
            </tbody>
          </table>
        </div>
      )}

      <h2>By purpose</h2>
      {(d.byPurpose?.length ?? 0) === 0 ? (
        <p className="muted">No usage yet.</p>
      ) : (
        <div className="table-wrap">
          <table className="table" data-testid="purpose-table">
            <thead>
              <tr>
                <th scope="col">Purpose</th>
                <th scope="col" className="num">Calls</th>
                <th scope="col" className="num">Input tokens</th>
                <th scope="col" className="num">Output tokens</th>
                <th scope="col" className="num">Cost</th>
              </tr>
            </thead>
            <tbody>
              {d.byPurpose?.map((r) => (
                <tr key={r.purpose}>
                  <td>{r.purpose}</td>
                  <td className="num">{formatInt(r.calls)}</td>
                  <td className="num">{formatInt(r.inputTokens)}</td>
                  <td className="num">{formatInt(r.outputTokens)}</td>
                  <td className="num">{formatUsd(r.costUsd)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

function ProviderRow({ provider: p, busy, onPatch }: { provider: AiProvider; busy: boolean; onPatch: (patch: ProviderPatch) => void }) {
  const [priority, setPriority] = useState(String(p.priority ?? 0))
  const [limit, setLimit] = useState(p.spendLimitUsd === null || p.spendLimitUsd === undefined ? '' : String(p.spendLimitUsd))

  const commitPriority = useCallback(() => {
    const n = Number(priority)
    if (!Number.isInteger(n) || n === (p.priority ?? 0)) return
    onPatch({ priority: n })
  }, [priority, p.priority, onPatch])

  const commitLimit = useCallback(() => {
    const trimmed = limit.trim()
    const next = trimmed === '' ? null : Number(trimmed)
    if (next !== null && (Number.isNaN(next) || next < 0)) return
    if (next === (p.spendLimitUsd ?? null)) return
    onPatch({ spendLimitUsd: next })
  }, [limit, p.spendLimitUsd, onPatch])

  const exceeded = p.exceeded ?? (p.spendLimitUsd !== null && p.spendLimitUsd !== undefined && (p.spentUsd ?? 0) >= p.spendLimitUsd)
  return (
    <tr className={`${p.enabled ? '' : 'is-muted'}`} data-testid="provider-row">
      <td>
        <strong>{p.name}</strong>
        {exceeded && <span className="badge badge--danger"> cap reached</span>}
      </td>
      <td>
        <code className="small">{p.model ?? '—'}</code>
      </td>
      <td>
        <label className="switch">
          <input type="checkbox" checked={!!p.enabled} disabled={busy} onChange={(e) => onPatch({ enabled: e.target.checked })} aria-label={`Enable ${p.name}`} />
          <span className="switch__track" aria-hidden="true" />
        </label>
      </td>
      <td>
        <input type="number" className="input--sm" value={priority} min={0} step={1} aria-label={`Priority for ${p.name}`} onChange={(e) => setPriority(e.target.value)} onBlur={commitPriority} onKeyDown={(e) => e.key === 'Enter' && commitPriority()} disabled={busy} />
      </td>
      <td>
        <input type="number" className="input--sm" value={limit} min={0} step="0.01" placeholder="no cap" aria-label={`Spend limit for ${p.name}`} onChange={(e) => setLimit(e.target.value)} onBlur={commitLimit} onKeyDown={(e) => e.key === 'Enter' && commitLimit()} disabled={busy} />
      </td>
      <td className="num">{formatUsd(p.spentUsd)}</td>
    </tr>
  )
}
