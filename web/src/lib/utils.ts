import { clsx, type ClassValue } from 'clsx'
import { extendTailwindMerge } from 'tailwind-merge'

/* The typography scale in index.css (text-body-sm, text-caption, …) is
   invisible to tailwind-merge, whose text-color group accepts any
   `text-*` class. Without this, a later size class silently deletes an
   earlier color class through cn() — e.g. size="sm" buttons lost the
   text-white from their variant and rendered blue-on-black ink. */
const twMerge = extendTailwindMerge({
  extend: {
    classGroups: {
      'font-size': [
        {
          text: [
            'display-xl',
            'display-lg',
            'display-md',
            'display-sm',
            'body-lg',
            'body-md',
            'body-sm',
            'label-md',
            'caption',
            'ref',
            'ref-lg',
            'figure',
            'eyebrow',
          ],
        },
      ],
    },
  },
})

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
