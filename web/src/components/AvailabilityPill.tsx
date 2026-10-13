import { useMemo } from 'react'
import { availabilityLabel } from '../lib/format'

type Props = { available?: number | null; copies?: number | null; holdsWaiting?: number | null; className?: string }

export function AvailabilityPill(props: Props) {
  const { text, tone } = useMemo(
    () => availabilityLabel({ available: props.available, copies: props.copies, holdsWaiting: props.holdsWaiting }),
    [props.available, props.copies, props.holdsWaiting],
  )
  return (
    <span className={`pill pill--${tone} ${props.className ?? ''}`} data-testid="availability">
      {text}
    </span>
  )
}
