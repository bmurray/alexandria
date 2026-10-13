import type { Role } from '../schemas/auth'

const RANK: Record<Role, number> = { member: 1, librarian: 2, admin: 3 }

/** True when `role` is at least `min` in the admin > librarian > member order. */
export function hasRole(role: Role | null | undefined, min: Role): boolean {
  if (!role) return false
  return (RANK[role] ?? 0) >= RANK[min]
}

export const ROLES: Role[] = ['member', 'librarian', 'admin']
