import { useCallback, useMemo, useRef, useState, type KeyboardEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, api, errorMessage } from '../api/client'
import type { ChatMessage, ConversationDetail, ToolCall } from '../schemas/ai'
import { useAuth } from '../hooks/useAuth'
import { useDrawer } from '../hooks/useDrawer'
import { useToast } from '../hooks/useToast'
import { formatUsd, timeAgo } from '../lib/format'
import { Markdown } from './Markdown'
import { ChatIcon, CloseIcon, HistoryIcon, PlusIcon, SendIcon, TrashIcon } from './icons'
import { Loading } from './States'

const OPENERS = ["What's good for a rainy weekend?", 'Do you have anything by Ursula K. Le Guin?', 'Is Dune available?']

function toolSummary(calls: ToolCall[] | null | undefined): string | null {
  if (!calls || calls.length === 0) return null
  return calls.map((c) => c.name).join(', ')
}

function chatError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 503) return 'The librarian is away right now: no AI provider is available.'
    if (err.status === 429) return "Today's AI budget is used up. Please try again tomorrow."
  }
  return errorMessage(err, 'The librarian could not answer')
}

/** "Ask the Librarian": a floating button and a right-side chat drawer on every page. */
export function LibrarianDrawer() {
  const { open, setOpen, toggle, draft, setDraft } = useDrawer()
  return (
    <>
      <button
        type="button"
        className={`ask-fab ${open ? 'is-open' : ''}`}
        onClick={toggle}
        aria-expanded={open}
        aria-controls="librarian-drawer"
        aria-label="Ask the Librarian"
        data-testid="ask-librarian"
      >
        <ChatIcon />
        <span className="ask-fab__label">Ask the Librarian</span>
      </button>
      {open && <button type="button" className="scrim" aria-label="Close the librarian panel" onClick={() => setOpen(false)} />}
      <aside
        id="librarian-drawer"
        className={`drawer ${open ? 'is-open' : ''}`}
        role="dialog"
        aria-label="Ask the Librarian"
        aria-hidden={!open}
        onKeyDown={(e) => {
          if (e.key === 'Escape') setOpen(false)
        }}
      >
        {open && <DrawerBody draft={draft} setDraft={setDraft} close={() => setOpen(false)} />}
      </aside>
    </>
  )
}

type BodyProps = { draft: string; setDraft: (s: string) => void; close: () => void }

function DrawerBody({ draft, setDraft, close }: BodyProps) {
  const qc = useQueryClient()
  const { user, can } = useAuth()
  const toast = useToast()
  const [conversationId, setConversationId] = useState<string | null>(null)
  const [view, setView] = useState<'chat' | 'history'>('chat')
  const [lastError, setLastError] = useState<string | null>(null)
  const textarea = useRef<HTMLTextAreaElement>(null)

  const conversation = useQuery({
    queryKey: ['ai', 'conversation', conversationId ?? ''] as const,
    queryFn: api.conversation,
    enabled: conversationId !== null,
    staleTime: 60 * 1000,
  })
  const list = useQuery({
    queryKey: ['ai', 'conversations'] as const,
    queryFn: api.conversations,
    enabled: view === 'history',
  })

  const send = useMutation({
    mutationFn: (message: string) => api.chat({ conversationId: conversationId ?? undefined, message }),
    onMutate: () => setLastError(null),
    onSuccess: (res, message) => {
      const now = new Date().toISOString()
      const userMsg: ChatMessage = { id: `local-u-${Date.now()}`, role: 'user', content: message, createdAt: now }
      const reply: ChatMessage = {
        id: `local-a-${Date.now()}`,
        role: 'assistant',
        content: res.reply,
        toolCalls: res.toolCalls ?? [],
        usage: res.usage ?? null,
        createdAt: now,
      }
      qc.setQueryData<ConversationDetail>(['ai', 'conversation', res.conversationId], (old) => ({
        conversation: old?.conversation ?? { id: res.conversationId },
        messages: [...(old?.messages ?? []), userMsg, reply],
      }))
      setConversationId(res.conversationId)
      void qc.invalidateQueries({ queryKey: ['ai', 'conversations'] })
      // A hold placed through the chat shows up on the catalog.
      void qc.invalidateQueries({ queryKey: ['books'] })
      void qc.invalidateQueries({ queryKey: ['me'] })
      textarea.current?.focus()
    },
    onError: (err) => setLastError(chatError(err)),
  })

  const remove = useMutation({
    mutationFn: (id: string) => api.deleteConversation(id),
    onSuccess: (_d, id) => {
      qc.removeQueries({ queryKey: ['ai', 'conversation', id] })
      void qc.invalidateQueries({ queryKey: ['ai', 'conversations'] })
      if (id === conversationId) setConversationId(null)
      toast.push('success', 'Conversation deleted')
    },
    onError: (err) => toast.push('error', errorMessage(err, 'Could not delete')),
  })

  const submit = useCallback(() => {
    const text = draft.trim()
    if (!text || send.isPending) return
    setDraft('')
    send.mutate(text)
  }, [draft, send, setDraft])

  const onKey = useCallback(
    (e: KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault()
        submit()
      }
    },
    [submit],
  )

  const startNew = useCallback(() => {
    setConversationId(null)
    setView('chat')
    setLastError(null)
    setDraft('')
    textarea.current?.focus()
  }, [setDraft])

  const pick = useCallback((id: string) => {
    setConversationId(id)
    setView('chat')
    setLastError(null)
  }, [])

  // Rendered newest-first inside a column-reverse list so it sticks to the bottom
  // without a scroll effect.
  const messages = useMemo(() => {
    const ms = conversationId ? (conversation.data?.messages ?? []) : []
    return [...ms].reverse()
  }, [conversation.data, conversationId])

  const pendingMessage = send.isPending ? send.variables : null
  const isAdmin = can('admin')
  const showOpeners = messages.length === 0 && !pendingMessage && !conversation.isPending

  return (
    <div className="drawer__inner">
      <header className="drawer__head">
        <div>
          <h2 className="drawer__title">Ask the Librarian</h2>
          <p className="drawer__sub">Knows the whole catalog. {user ? 'Can place holds for you.' : 'Sign in and it can place holds.'}</p>
        </div>
        <div className="drawer__tools">
          <button type="button" className="icon-btn" onClick={() => setView(view === 'history' ? 'chat' : 'history')} aria-label="Conversation history" aria-pressed={view === 'history'} title="History">
            <HistoryIcon />
          </button>
          <button type="button" className="icon-btn" onClick={startNew} aria-label="New conversation" title="New conversation">
            <PlusIcon />
          </button>
          <button type="button" className="icon-btn" onClick={close} aria-label="Close" title="Close">
            <CloseIcon />
          </button>
        </div>
      </header>

      {view === 'history' ? (
        <div className="drawer__history">
          {list.isPending ? (
            <Loading label="Loading conversations…" />
          ) : list.isError ? (
            <p className="muted">Could not load your conversations.</p>
          ) : (list.data?.conversations.length ?? 0) === 0 ? (
            <p className="muted">No conversations yet. Ask something below.</p>
          ) : (
            <ul className="conv-list">
              {list.data?.conversations.map((c) => (
                <li key={c.id} className={`conv-list__item ${c.id === conversationId ? 'is-active' : ''}`}>
                  <button type="button" className="conv-list__open" onClick={() => pick(c.id)}>
                    <span className="conv-list__title">{c.title || 'Untitled conversation'}</span>
                    <span className="small muted">{timeAgo(c.updatedAt ?? c.createdAt)}</span>
                  </button>
                  <button type="button" className="icon-btn" aria-label={`Delete ${c.title || 'conversation'}`} onClick={() => remove.mutate(c.id)} disabled={remove.isPending}>
                    <TrashIcon />
                  </button>
                </li>
              ))}
            </ul>
          )}
          <button type="button" className="btn btn--sm" onClick={() => setView('chat')}>
            Back to chat
          </button>
        </div>
      ) : (
        <div className="drawer__messages" aria-live="polite" aria-busy={send.isPending}>
          {lastError && (
            <div className="chat-msg chat-msg--error" role="alert">
              {lastError}
            </div>
          )}
          {pendingMessage && (
            <>
              <div className="chat-msg chat-msg--assistant chat-msg--thinking" aria-label="The librarian is thinking">
                <span className="dots" aria-hidden="true">
                  <i />
                  <i />
                  <i />
                </span>
              </div>
              <div className="chat-msg chat-msg--user">{pendingMessage}</div>
            </>
          )}
          {messages.map((m) => (
            <MessageBubble key={m.id} message={m} isAdmin={isAdmin} />
          ))}
          {conversationId && conversation.isPending && <Loading label="Opening conversation…" />}
          {showOpeners && (
            <div className="openers">
              <p className="muted">Hello. I know every shelf here. Try one of these, or ask your own question.</p>
              <div className="openers__chips">
                {OPENERS.map((o) => (
                  <button key={o} type="button" className="chip" onClick={() => { setDraft(''); send.mutate(o) }}>
                    {o}
                  </button>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      <form
        className="composer"
        onSubmit={(e) => {
          e.preventDefault()
          submit()
        }}
      >
        <label htmlFor="librarian-input" className="visually-hidden">
          Message the librarian
        </label>
        <textarea
          id="librarian-input"
          ref={textarea}
          rows={2}
          placeholder="Ask about a book, an author, or what to read next…"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={onKey}
          disabled={send.isPending}
          autoFocus
        />
        <button type="submit" className="btn btn--primary composer__send" disabled={!draft.trim() || send.isPending} aria-label="Send">
          <SendIcon />
          <span>Send</span>
        </button>
        <span className="composer__hint small muted">Enter to send · Shift+Enter for a new line</span>
      </form>
    </div>
  )
}

function MessageBubble({ message, isAdmin }: { message: ChatMessage; isAdmin: boolean }) {
  const tools = toolSummary(message.toolCalls)
  if (message.role === 'user') return <div className="chat-msg chat-msg--user">{message.content ?? ''}</div>
  const u = message.usage
  return (
    <div className="chat-msg chat-msg--assistant">
      <Markdown text={message.content ?? ''} />
      {tools && (
        <details className="chat-msg__tools">
          <summary>looked up: {tools}</summary>
          <ul>
            {message.toolCalls?.map((c, i) => (
              <li key={i}>
                <code>{c.name}</code> {c.input !== undefined && <span className="muted">{JSON.stringify(c.input)}</span>}
              </li>
            ))}
          </ul>
        </details>
      )}
      {isAdmin && u && (
        <p className="chat-msg__usage">
          {u.provider}
          {u.model ? ` · ${u.model}` : ''} · {u.inputTokens ?? 0} in / {u.outputTokens ?? 0} out · {formatUsd(u.costUsd)}
        </p>
      )}
    </div>
  )
}
