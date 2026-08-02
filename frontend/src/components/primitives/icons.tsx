import type { SVGProps } from "react";

/**
 * Inline SVG icons. Every icon is decorative (`aria-hidden`): the surrounding
 * markup always carries the accessible text.
 */

type IconProps = SVGProps<SVGSVGElement> & { size?: number };

function base({ size = 16, ...rest }: IconProps): SVGProps<SVGSVGElement> {
  return {
    width: size,
    height: size,
    viewBox: "0 0 24 24",
    fill: "none",
    stroke: "currentColor",
    strokeWidth: 2,
    strokeLinecap: "round",
    strokeLinejoin: "round",
    "aria-hidden": true,
    focusable: false,
    ...rest,
  };
}

/** ForgeMarkIcon is the brand mark: an anvil. */
export function ForgeMarkIcon(props: IconProps) {
  return (
    <svg {...base(props)}>
      <path d="M3 6h13c0 3-2 5-5 5v2c0 2 2 4 5 5v1H6v-1c3-1 5-3 5-5v-2H8C5 11 3 9 3 6Z" />
      <path d="M16 6h5" />
    </svg>
  );
}

/** WarningIcon is a rounded warning triangle for banners. */
export function WarningIcon(props: IconProps) {
  return (
    <svg {...base(props)}>
      <path d="M10.3 4.1 2.6 18a2 2 0 0 0 1.7 3h15.4a2 2 0 0 0 1.7-3L13.7 4.1a2 2 0 0 0-3.4 0Z" />
      <path d="M12 9v4" />
      <path d="M12 17h.01" />
    </svg>
  );
}

/** InboxIcon marks the "no projects" empty state. */
export function InboxIcon(props: IconProps) {
  return (
    <svg {...base(props)}>
      <path d="M22 12h-6l-2 3h-4l-2-3H2" />
      <path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11Z" />
    </svg>
  );
}

/** MoonIcon marks the calm "nothing running" empty state. */
export function MoonIcon(props: IconProps) {
  return (
    <svg {...base(props)}>
      <path d="M21 12.8A9 9 0 1 1 11.2 3 7 7 0 0 0 21 12.8Z" />
    </svg>
  );
}

/** CopyIcon decorates copy-to-clipboard affordances. */
export function CopyIcon(props: IconProps) {
  return (
    <svg {...base(props)}>
      <rect width="13" height="13" x="9" y="9" rx="2" />
      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
    </svg>
  );
}
