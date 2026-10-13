import { z } from 'zod'

export const bookSchema = z.object({
  id: z.string(),
  title: z.string(),
  author: z.string().nullable().optional(),
  isbn: z.string().nullable().optional(),
  year: z.number().nullable().optional(),
  genre: z.string().nullable().optional(),
  publisher: z.string().nullable().optional(),
  description: z.string().nullable().optional(),
  coverUrl: z.string().nullable().optional(),
  copies: z.number().nullable().optional(),
  tags: z.array(z.string()).nullable().optional(),
  createdAt: z.string().nullable().optional(),
  updatedAt: z.string().nullable().optional(),
  available: z.number().nullable().optional(),
  onLoan: z.number().nullable().optional(),
  holdsWaiting: z.number().nullable().optional(),
})
export type Book = z.infer<typeof bookSchema>

export const bookListSchema = z.object({
  books: z.array(bookSchema),
  total: z.number().nullable().optional(),
})
export type BookList = z.infer<typeof bookListSchema>

export const bookResponseSchema = z.object({ book: bookSchema })

export const genreListSchema = z.object({
  genres: z.array(z.object({ name: z.string(), count: z.number().nullable().optional() })),
})
export type Genre = z.infer<typeof genreListSchema>['genres'][number]

/** What the librarian sends on create; PATCH sends any subset. */
export const bookInputSchema = z.object({
  title: z.string(),
  author: z.string(),
  isbn: z.string(),
  year: z.number().nullable(),
  genre: z.string(),
  publisher: z.string(),
  description: z.string(),
  coverUrl: z.string(),
  copies: z.number(),
  tags: z.array(z.string()),
})
export type BookInput = z.infer<typeof bookInputSchema>
export type BookPatch = Partial<BookInput>

export const sortSchema = z.enum(['title', 'author', 'year', 'added'])
export type Sort = z.infer<typeof sortSchema>

export type BookListParams = {
  q: string
  genre: string
  available: boolean
  sort: Sort
  page: number
}

/** Enrich / lookup answers: a suggestion the librarian applies field by field. */
export const suggestionSchema = z.object({
  title: z.string().nullable().optional(),
  author: z.string().nullable().optional(),
  isbn: z.string().nullable().optional(),
  year: z.number().nullable().optional(),
  genre: z.string().nullable().optional(),
  publisher: z.string().nullable().optional(),
  description: z.string().nullable().optional(),
  coverUrl: z.string().nullable().optional(),
  tags: z.array(z.string()).nullable().optional(),
})
export type Suggestion = z.infer<typeof suggestionSchema>

export const suggestionResponseSchema = z.object({
  suggestion: suggestionSchema.nullable().optional(),
  source: z.string().nullable().optional(),
})
export type SuggestionResponse = z.infer<typeof suggestionResponseSchema>
