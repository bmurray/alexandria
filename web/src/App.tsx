import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { AuthProvider } from './hooks/AuthProvider'
import { ToastProvider } from './hooks/ToastProvider'
import { DrawerProvider } from './hooks/DrawerProvider'
import { LiveBooksProvider } from './hooks/LiveBooksProvider'
import { Layout } from './components/Layout'
import { RequireRole } from './components/RequireRole'
import { Catalog } from './routes/Catalog'
import { Book } from './routes/Book'
import { BookEdit } from './routes/BookEdit'
import { Loans } from './routes/Loans'
import { Dashboard } from './routes/Dashboard'
import { Holds } from './routes/Holds'
import { Users } from './routes/Users'
import { AiSpend } from './routes/AiSpend'
import { Login } from './routes/Login'
import { AuthCallback } from './routes/AuthCallback'
import { Account } from './routes/Account'
import { NotFound } from './routes/NotFound'

const queryClient = new QueryClient({
  defaultOptions: { queries: { refetchOnWindowFocus: false, retry: 1, staleTime: 10 * 1000 } },
})

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <AuthProvider>
          <BrowserRouter>
            <DrawerProvider>
              <LiveBooksProvider>
                <Routes>
                  <Route element={<Layout />}>
                    <Route path="/" element={<Catalog />} />
                    <Route path="/books/:id" element={<Book />} />
                    <Route path="/login" element={<Login />} />
                    <Route path="/auth/callback" element={<AuthCallback />} />
                    <Route path="/me/loans" element={<Navigate to="/loans" replace />} />

                    <Route element={<RequireRole min="member" />}>
                      <Route path="/loans" element={<Loans />} />
                      <Route path="/account" element={<Account />} />
                    </Route>

                    <Route element={<RequireRole min="librarian" />}>
                      <Route path="/books/new" element={<BookEdit />} />
                      <Route path="/books/:id/edit" element={<BookEdit />} />
                      <Route path="/dashboard" element={<Dashboard />} />
                      <Route path="/holds" element={<Holds />} />
                    </Route>

                    <Route element={<RequireRole min="admin" />}>
                      <Route path="/users" element={<Users />} />
                      <Route path="/ai" element={<AiSpend />} />
                    </Route>

                    <Route path="*" element={<NotFound />} />
                  </Route>
                </Routes>
              </LiveBooksProvider>
            </DrawerProvider>
          </BrowserRouter>
        </AuthProvider>
      </ToastProvider>
    </QueryClientProvider>
  )
}
