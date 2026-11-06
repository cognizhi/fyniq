import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, ImagePlus, Palette, ShieldCheck, Trash2, Upload } from "lucide-react";
import { useId, useRef, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { SettingsPage } from "./SettingsPage";

type Brand = components["schemas"]["Brand"];
type Kind = "light" | "dark" | "icon";

const kinds: { kind: Kind; title: string; hint: string }[] = [
  { kind: "light", title: "For light backgrounds", hint: "Your main logo: the app, emails, documents and reports." },
  { kind: "dark", title: "For dark backgrounds", hint: "Until you add one, dark screens show the light logo on a white tile." },
  { kind: "icon", title: "Square icon", hint: "For the browser tab and the app icon: a square SVG, or a PNG at least 192 pixels." },
];

export async function brandOf(): Promise<Brand> {
  const { data, error } = await api.GET("/v1/organisation/brand");
  if (error) throw error;
  return data;
}

// useBrandImage is a data URL of one of the brand's files, of the version in force; undefined while there is none.
export function useBrandImage(brand: Brand | undefined, kind: Kind) {
  const file = brand?.[kind];
  return useQuery({
    queryKey: ["brand-file", kind, brand?.version, file?.sha256],
    enabled: Boolean(file),
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/organisation/brand/files/{kind}", { params: { path: { kind } } });
      if (error) throw error;
      return `data:${data.media_type};base64,${data.content_base64}`;
    },
  });
}

// Slot is one of the brand's files: its preview, what was cleaned, and Replace and Remove for those who may change it.
function Slot({ brand, kind, title, hint, may, onChanged }: { brand: Brand; kind: Kind; title: string; hint: string; may: boolean; onChanged: (p: ApiProblem | null) => void }) {
  const id = useId();
  const input = useRef<HTMLInputElement>(null);
  const confirm = useConfirm();
  const image = useBrandImage(brand, kind);
  const [busy, setBusy] = useState(false);
  const file = brand[kind];
  const upload = helpText("brand.upload");

  const send = async (f: File) => {
    setBusy(true);
    const key = newKey();
    // The generated body names the form's field; the request sends it as multipart/form-data with the file itself.
    const bodySerializer = () => {
      const form = new FormData();
      form.append("file", f, f.name);
      return form;
    };
    const out = await guarded(confirm, () =>
      api.PUT("/v1/organisation/brand/files/{kind}", {
        params: { path: { kind }, header: { "If-Match": `"${brand.version}"`, "Idempotency-Key": key } },
        body: { file: f.name },
        bodySerializer,
      }),
    );
    setBusy(false);
    onChanged(out.data ? null : problemOf(out.error));
  };
  const remove = async () => {
    setBusy(true);
    const out = await guarded(confirm, () =>
      api.DELETE("/v1/organisation/brand/files/{kind}", { params: { path: { kind }, header: { "If-Match": `"${brand.version}"` } } }),
    );
    setBusy(false);
    onChanged(out.data ? null : problemOf(out.error));
  };

  return (
    <div className="fy-stack fy-stack--3" style={{ flex: "1 1 200px" }}>
      <div
        className={file ? "grid h-[120px] place-items-center rounded-md border border-border" : "fy-dropzone h-[120px]"}
        style={kind === "dark" && file ? { background: "var(--text-primary)" } : undefined}
      >
        {image.data ? <img src={image.data} alt={`${title}: your logo`} style={{ maxHeight: 64, maxWidth: "80%" }} /> : <Icon of={ImagePlus} size={24} />}
      </div>
      <div className="fy-stack fy-stack--2">
        <span className="fy-row fy-row--wrap gap-2">
          <span className="fy-strong" id={`${id}-title`}>
            {title}
          </span>
          {file?.cleaned && (
            <span className="fy-chip fy-chip--good">
              <Icon of={ShieldCheck} />
              Cleaned
            </span>
          )}
        </span>
        <span className="fy-small fy-muted">
          {file ? `${file.media_type === "image/svg+xml" ? "SVG" : "PNG"} · ${Math.max(1, Math.round(file.size_bytes / 1024))} KB` : hint}
        </span>
        {file?.cleaned && (
          <span className="fy-small fy-secondary">
            We removed {file.cleaned.elements?.length ? file.cleaned.elements.join(", ") : "nothing visible"}
            {file.cleaned.attributes ? `, ${file.cleaned.attributes} unsafe attribute${file.cleaned.attributes === 1 ? "" : "s"}` : ""}
            {file.cleaned.external ? ` and ${file.cleaned.external} outside reference${file.cleaned.external === 1 ? "" : "s"}` : ""}, and kept the shapes.
          </span>
        )}
      </div>
      {may && (
        <div className="fy-row gap-2">
          <input
            ref={input}
            className="fy-sr"
            type="file"
            accept=".svg,.png,image/svg+xml,image/png"
            aria-labelledby={`${id}-title`}
            onChange={(e) => {
              const f = e.target.files?.[0];
              if (f) void send(f);
              e.target.value = "";
            }}
          />
          <Button size="sm" variant="secondary" disabled={busy} onClick={() => input.current?.click()} title={upload.tooltip}>
            <Icon of={Upload} />
            {file ? "Replace" : upload.label}
          </Button>
          {file && (
            <Button size="sm" variant="ghost" disabled={busy} onClick={() => void remove()} aria-label={`Remove the logo ${title.toLowerCase()}`}>
              <Icon of={Trash2} />
              Remove
            </Button>
          )}
        </div>
      )}
    </div>
  );
}

// BrandPage is Settings › Logo and brand (docs/design/screens LogoBrand; FR-ORG-09, PRD-001 rev 0.200): the logo for light
// and dark backgrounds, the square icon and the accent colour of documents and emails. Each change is the next version
// and takes effect at once; an SVG is cleaned first and the page says what went.
export function BrandPage() {
  const { session } = useSignedIn();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const brand = useQuery({ queryKey: ["brand"], queryFn: brandOf });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const roles = new Set(session.roles ?? []);
  const may = roles.has("owner") || roles.has("administrator") || roles.has("controller");
  const b = brand.data;

  const changed = async (p: ApiProblem | null) => {
    setProblem(p);
    await queries.invalidateQueries({ queryKey: ["brand"] });
  };
  const setAccent = async (accent: string) => {
    if (!b) return;
    setBusy(true);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.PUT("/v1/organisation/brand/accent", { params: { header: { "If-Match": `"${b.version}"`, "Idempotency-Key": key } }, body: { accent } }),
    );
    setBusy(false);
    await changed(out.data ? null : problemOf(out.error));
  };

  return (
    <SettingsPage title="Logo and brand" screen="logo-brand" lead="Upload your logo once. It changes everywhere at the same time.">
      {problem && <Refusal problem={problem} title="The brand did not change" />}
      {brand.isError && <Refusal problem={problemOf(brand.error)} title="The brand could not be read" />}
      {b && (
        <>
          <section className="fy-card" aria-labelledby="files-title">
            <div className="fy-card__head">
              <h2 className="fy-h3" id="files-title">
                Logo files
              </h2>
              <span className="fy-small fy-muted fy-from-medium">SVG or PNG · PNG at least 512 px wide</span>
            </div>
            <div className="fy-card__body">
              <div className="fy-row fy-row--wrap fy-row--top gap-4">
                {kinds.map((k) => (
                  <Slot key={k.kind} brand={b} kind={k.kind} title={k.title} hint={k.hint} may={may} onChanged={changed} />
                ))}
              </div>
            </div>
            <div className="fy-card__foot">
              <span className="fy-small fy-secondary">
                {b.version ? `Version ${b.version}${b.updated_at ? `, saved ${dateTime(b.updated_at)}` : ""}. ` : ""}
                SVGs lose scripts, event handlers, outside references, embedded images and fonts. Documents already issued keep the logo they were issued with.
              </span>
            </div>
          </section>
          <section className="fy-card" aria-labelledby="accent-title">
            <div className="fy-card__head">
              <h2 className="fy-h3" id="accent-title">
                Accent colour
              </h2>
            </div>
            <div className="fy-card__body fy-stack fy-stack--3">
              <p className="fy-small fy-secondary">For documents and emails only: the app keeps its own colours. Each option reads clearly against white.</p>
              <div className="fy-row fy-row--wrap gap-2" role="radiogroup" aria-labelledby="accent-title">
                {(b.accent_options ?? []).map((o) => (
                  <button
                    key={o.colour}
                    type="button"
                    role="radio"
                    aria-checked={(b.accent ?? "") === o.colour}
                    disabled={!may || busy}
                    className="fy-chip"
                    onClick={() => void setAccent(o.colour)}
                  >
                    {/* the swatch shows the option's own colour, which is data from the API */}
                    <span aria-hidden="true" style={{ display: "inline-block", width: 14, height: 14, borderRadius: 4, background: o.colour }} />
                    {o.name}
                    {(b.accent ?? "") === o.colour && <Icon of={Check} />}
                  </button>
                ))}
                <button type="button" role="radio" aria-checked={!b.accent} disabled={!may || busy} className="fy-chip" onClick={() => void setAccent("")}>
                  <Icon of={Palette} />
                  The default
                </button>
              </div>
            </div>
          </section>
        </>
      )}
    </SettingsPage>
  );
}

// CompanyLogo is the organisation's logo in the app's header (FR-ORG-09): the one for dark backgrounds in a dark theme
// when there is one, else the light one; nothing until a logo is uploaded.
export function CompanyLogo({ name }: { name?: string }) {
  const brand = useQuery({ queryKey: ["brand"], queryFn: brandOf, staleTime: 60_000 });
  const dark = typeof document !== "undefined" && document.documentElement.classList.contains("dark") && Boolean(brand.data?.dark);
  const image = useBrandImage(brand.data, dark ? "dark" : "light");
  if (!image.data) return null;
  return <img className="fy-from-medium" src={image.data} alt={name ? `${name} logo` : "Company logo"} style={{ height: 24, width: "auto", maxWidth: 140 }} />;
}

