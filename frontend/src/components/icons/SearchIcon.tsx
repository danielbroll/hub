import { SVGAttributes } from "react";

export function SearchIcon(props: SVGAttributes<SVGElement>) {
  return (
    <svg
      width="24"
      height="24"
      viewBox="0 0 24 24"
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      {...props}
    >
      <path
        fill-rule="evenodd"
        clip-rule="evenodd"
        d="M10.5 5.5a5 5 0 100 10 5 5 0 000-10zm-6.5 5a6.5 6.5 0 1113 0 6.5 6.5 0 01-13 0z"
      />
      <path
        fill-rule="evenodd"
        clip-rule="evenodd"
        d="M14.47 14.47a.75.75 0 011.06 0l4 4a.75.75 0 11-1.06 1.06l-4-4a.75.75 0 010-1.06z"
      />
    </svg>
  );
}
