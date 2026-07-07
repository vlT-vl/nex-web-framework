import centuryGothicUrl from "../../../res/CenturyGothic.ttf?url";
import "./nexweblogo.css";

export function NexWebLogo({ animated = true, className = "", size = "main" }) {
  const classes = [
    "nex-logo",
    "nex-web-logo",
    `nex-logo-${size}`,
    animated ? "is-animated" : "",
    className,
  ].filter(Boolean).join(" ");

  return (
    <>
      <style>{`
        @font-face {
          font-family: "Century Gothic nex";
          src: url("${centuryGothicUrl}") format("truetype");
          font-weight: 400;
          font-style: normal;
          font-display: swap;
        }
      `}</style>
      <div className={classes} aria-label="nex-web" role="img">
        <img className="nex-logo-cube" src="/nexcube.svg" alt="" aria-hidden="true" />
        <span className="nex-logo-word">nex</span>
        <span className="nex-logo-suffix">web</span>
      </div>
    </>
  );
}
