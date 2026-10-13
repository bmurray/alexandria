import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, errorMessage } from '../api/client'
import { type Role, type User } from '../schemas/auth'
import { useAuth } from '../hooks/useAuth'
import { useToast } from '../hooks/useToast'
import { ROLES } from '../lib/roles'
import { formatDate, initials, timeAgo } from '../lib/format'
import { Empty, ErrorState, Loading } from '../components/States'

export function Users() {
  const { user: me } = useAuth()
  const qc = useQueryClient()
  const toast = useToast()
  const users = useQuery({ queryKey: ['users'] as const, queryFn: api.users })
  const setRole = useMutation({
    mutationFn: ({ id, role }: { id: string; role: Role }) => api.setUserRole(id, role),
    onMutate: async ({ id, role }) => {
      await qc.cancelQueries({ queryKey: ['users'] })
      const prev = qc.getQueryData<{ users: User[] }>(['users'])
      qc.setQueryData<{ users: User[] }>(['users'], (old) => old && { users: old.users.map((u) => (u.id === id ? { ...u, role } : u)) })
      return { prev }
    },
    onError: (err, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(['users'], ctx.prev)
      toast.push('error', errorMessage(err, 'Could not change the role'))
    },
    onSuccess: (res) => toast.push('success', `${res.user.name || res.user.email} is now ${res.user.role}`),
    onSettled: () => void qc.invalidateQueries({ queryKey: ['users'] }),
  })

  if (users.isPending) return <Loading label="Loading members…" />
  if (users.isError) return <ErrorState error={users.error} onRetry={() => void users.refetch()} />

  return (
    <section className="page-section">
      <h1 className="display">Members</h1>
      <p className="muted">Roles are ordered admin &gt; librarian &gt; member. You cannot change your own role.</p>
      {users.data.users.length === 0 ? (
        <Empty title="No members yet" />
      ) : (
        <div className="table-wrap">
          <table className="table" data-testid="users-table">
            <thead>
              <tr>
                <th scope="col">Member</th>
                <th scope="col">Role</th>
                <th scope="col">Joined</th>
                <th scope="col">Last sign-in</th>
                <th scope="col">Id</th>
              </tr>
            </thead>
            <tbody>
              {users.data.users.map((u) => {
                const self = u.id === me?.id
                return (
                  <tr key={u.id} className={self ? 'is-self' : ''} data-testid="user-row">
                    <td>
                      <span className="who">
                        {u.picture ? <img className="avatar" src={u.picture} alt="" referrerPolicy="no-referrer" /> : <span className="avatar avatar--initials">{initials(u.name, u.email)}</span>}
                        <span>
                          <strong>{u.name || '—'}</strong>
                          {self && <span className="badge"> you</span>}
                          <br />
                          <span className="small muted">{u.email}</span>
                        </span>
                      </span>
                    </td>
                    <td>
                      <label className="visually-hidden" htmlFor={`role-${u.id}`}>
                        Role for {u.name || u.email}
                      </label>
                      <select id={`role-${u.id}`} value={u.role} disabled={self || setRole.isPending} onChange={(e) => setRole.mutate({ id: u.id, role: e.target.value as Role })}>
                        {ROLES.map((r) => (
                          <option key={r} value={r}>
                            {r}
                          </option>
                        ))}
                      </select>
                    </td>
                    <td>{formatDate(u.createdAt) || '—'}</td>
                    <td>{timeAgo(u.lastLoginAt) || '—'}</td>
                    <td>
                      <code className="small">{u.id}</code>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
