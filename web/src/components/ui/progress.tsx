/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
'use client'

import { Progress as ProgressPrimitive } from '@base-ui/react/progress'

import { cn } from '@/lib/utils'

function Progress({
  className,
  children,
  value,
  ...props
}: ProgressPrimitive.Root.Props) {
  return (
    <ProgressPrimitive.Root
      value={value}
      data-slot='progress'
      className={cn('flex flex-wrap gap-3', className)}
      {...props}
    >
      {children}
      <ProgressTrack>
        <ProgressIndicator />
      </ProgressTrack>
    </ProgressPrimitive.Root>
  )
}

function ProgressTrack({ className, ...props }: ProgressPrimitive.Track.Props) {
  return (
    <ProgressPrimitive.Track
      className={cn(
        'bg-muted relative flex h-1 w-full items-center overflow-x-hidden rounded-full',
        className
      )}
      data-slot='progress-track'
      {...props}
    />
  )
}

function ProgressIndicator({
  className,
  ...props
}: ProgressPrimitive.Indicator.Props) {
  return (
    <ProgressPrimitive.Indicator
      data-slot='progress-indicator'
      className={cn('bg-primary h-full transition-all', className)}
      {...props}
    />
  )
}

function ProgressLabel({ className, ...props }: ProgressPrimitive.Label.Props) {
  return (
    <ProgressPrimitive.Label
      className={cn('text-sm font-medium', className)}
      data-slot='progress-label'
      {...props}
    />
  )
}

function ProgressValue({ className, ...props }: ProgressPrimitive.Value.Props) {
  return (
    <ProgressPrimitive.Value
      className={cn(
        'text-muted-foreground ml-auto text-sm tabular-nums',
        className
      )}
      data-slot='progress-value'
      {...props}
    />
  )
}

function CircularProgress({
  className,
  children,
  value,
  ...props
}: Pick<
  ProgressPrimitive.Root.Props,
  'className' | 'children' | 'value' | 'aria-label' | 'aria-valuetext'
>) {
  const percent = Math.min(100, Math.max(0, value ?? 0))
  return (
    <ProgressPrimitive.Root
      value={value == null ? null : percent}
      className={cn(
        'relative grid size-10 shrink-0 place-items-center',
        className
      )}
      {...props}
    >
      <svg
        viewBox='0 0 40 40'
        className='absolute inset-0 size-full -rotate-90'
        aria-hidden='true'
      >
        <circle
          cx='20'
          cy='20'
          r='17'
          fill='none'
          stroke='currentColor'
          strokeWidth='3'
          className='text-muted'
        />
        <circle
          cx='20'
          cy='20'
          r='17'
          fill='none'
          stroke='currentColor'
          strokeWidth='3'
          strokeLinecap='round'
          pathLength='100'
          strokeDasharray='100'
          strokeDashoffset={100 - percent}
        />
      </svg>
      <span className='relative text-[10px] font-semibold tabular-nums'>
        {children}
      </span>
    </ProgressPrimitive.Root>
  )
}

export {
  CircularProgress,
  Progress,
  ProgressTrack,
  ProgressIndicator,
  ProgressLabel,
  ProgressValue,
}
