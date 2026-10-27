import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { Bike, Car, MapPin, Plus, Save, Trash2 } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { useCategories, type Claim, type ClaimLine } from "./claims";

type Draft = components["schemas"]["ExpenseClaimDraft"];
type Place = components["schemas"]["ClaimPlace"];

// TripForm is a trip as the member types it: the whole trip's distance as driven (UC-EXP-02; design D5).
export interface TripForm {
  key: string;
  date: string;
  purpose: string;
  from: string;
  to: string;
  return: boolean;
  vehicle: "car" | "motorcycle";
  distance: string;
}

export function newTrip(on: string): TripForm {
  return { key: newKey(), date: on, purpose: "", from: "", to: "", return: false, vehicle: "car", distance: "" };
}

// checkTrips reads the trips into claim lines for the mileage category, or names each field that cannot be read, all at
// once. The amount is the server's: the distance to 0.1 km at the vehicle's rate on the trip's date (RND-MILEAGE).
export function checkTrips(trips: TripForm[], category: string): { lines?: ClaimLine[]; errors: Record<string, string> } {
  const errors: Record<string, string> = {};
  const lines: ClaimLine[] = [];
  if (!category) errors.category = "The expense policy has no mileage category yet: ask the person who sets the policy to add it.";
  trips.forEach((t, i) => {
    if (!t.date) errors[`trips.${i}.date`] = "Enter the day of the trip.";
    if (!t.from.trim()) errors[`trips.${i}.from`] = "Enter where the trip started, or pick a saved place.";
    if (!t.to.trim()) errors[`trips.${i}.to`] = "Enter where the trip went, or pick a saved place.";
    if (!/^[0-9]{1,5}(\.[0-9]{1,3})?$/.test(t.distance.trim())) errors[`trips.${i}.distance`] = "Enter the distance driven in km, such as 18.4.";
    lines.push({
      kind: "mileage",
      date: t.date || undefined,
      category_id: category,
      description: t.purpose.trim() || undefined,
      from: t.from.trim(),
      to: t.to.trim(),
      return: t.return || undefined,
      vehicle: t.vehicle,
      distance: t.distance.trim(),
    });
  });
  if (trips.length === 0) errors.trips = "Add a trip.";
  return Object.keys(errors).length > 0 ? { errors } : { lines, errors };
}

function tripsOf(c: Claim): TripForm[] {
  return c.lines
    .filter((l) => l.kind === "mileage")
    .map((l) => ({ key: newKey(), date: l.date ?? "", purpose: l.description ?? "", from: l.from ?? "", to: l.to ?? "", return: Boolean(l.return), vehicle: l.vehicle ?? "car", distance: l.distance ?? "" }));
}

// MileageClaim is a claim's trips in the member's own car or motorcycle (docs/design/screens MileageClaim; FR-AP-16):
// a new mileage claim, or the trips of a draft claim. Each trip's distance is the whole trip, as driven, read from a map
// app; Fyniq sends no address anywhere. Saving keeps the claim's receipts as they are; the claim then shows what each
// trip comes to, and is submitted from it.
export function MileageClaim() {
  const { claimId } = useParams({ strict: false }) as { claimId?: string };
  const { organisation } = useSignedIn();
  const navigate = useNavigate();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const today = new Date().toISOString().slice(0, 10);
  const [title, setTitle] = useState("");
  const [trips, setTrips] = useState<TripForm[] | null>(claimId ? null : [newTrip(today)]);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const loaded = useQuery({
    queryKey: ["expense-claim", claimId],
    enabled: Boolean(claimId),
    queryFn: async () => {
      const { data, error, response } = await api.GET("/v1/expense-claims/{claim_id}", { params: { path: { claim_id: claimId ?? "" } } });
      if (error) throw error;
      return { claim: data, etag: etagOf(response) };
    },
  });
  const claim = loaded.data?.claim;
  const categories = useCategories();
  const mileage = (categories.data ?? []).find((c) => c.mileage && c.active);
  const places = useQuery({
    queryKey: ["claim-places"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/claim-places", { params: { query: { page_size: 100 } } });
      if (error) throw error;
      return data.items.filter((p) => p.active);
    },
  });
  useEffect(() => {
    if (claim && trips === null) {
      setTrips(tripsOf(claim));
      setTitle(claim.title ?? "");
    }
  }, [claim, trips]);

  const setTrip = (i: number, patch: Partial<TripForm>) => trips && setTrips(trips.map((t, j) => (j === i ? { ...t, ...patch } : t)));

  const save = async () => {
    if (!trips) return;
    const { lines, errors } = checkTrips(trips, mileage?.id ?? "");
    setErrors(errors);
    if (!lines) return;
    const receipts = (claim?.lines ?? []).filter((l) => l.kind !== "mileage");
    const latest = trips.reduce((d, t) => (t.date > d ? t.date : d), "");
    const body: Draft = {
      title: title.trim() || undefined,
      date: claim?.date && claim.date >= latest ? claim.date : latest || today,
      notes: claim?.notes || undefined,
      late_reason: claim?.late_reason || undefined,
      advance_id: claim?.advance_id || undefined,
      lines: [...receipts, ...lines],
    };
    setBusy(true);
    setProblem(null);
    const out = claim
      ? await guarded(confirm, () => api.PUT("/v1/expense-claims/{claim_id}", { params: { path: { claim_id: claim.id }, header: { "If-Match": loaded.data?.etag ?? "" } }, body }))
      : await guarded(confirm, () => api.POST("/v1/expense-claims", { params: { header: { "Idempotency-Key": newKey() } }, body }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["expense-claims"] });
    await queries.invalidateQueries({ queryKey: ["expense-claim", out.data.id] });
    await navigate({ to: "/me/claims/$claimId", params: { claimId: out.data.id } });
  };

  const save_ = helpText("claim.save");
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/me/claims">My claims</Link>
          </nav>
          <h1 className="fy-h1">Mileage trip</h1>
          <p className="fy-secondary">A business trip in your own vehicle, paid back at {organisation?.name ?? "the company"}'s rate per km.</p>
        </div>
      </div>
      {loaded.isError && <Refusal problem={problemOf(loaded.error)} title="The claim could not be opened" />}
      {problem && <Refusal problem={problem} title="The trips were not saved" />}
      {errors.category && <p className="fy-error">{errors.category}</p>}
      {trips && (
        <form
          className="fy-stack fy-stack--4"
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          {!claim && (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-title`}>
                What the trips were for (optional)
              </label>
              <input className="fy-input" id={`${id}-title`} value={title} maxLength={120} onChange={(e) => setTitle(e.target.value)} />
            </div>
          )}
          {trips.map((t, i) => (
            <fieldset key={t.key} className="fy-card fy-card--pad fy-stack fy-stack--3" disabled={busy} aria-label={`Trip ${i + 1}`}>
              <legend className="fy-h3">Trip {i + 1}</legend>
              <div className="fy-grid fy-grid--2">
                <TextField id={`${id}-${i}-date`} label="Trip date" type="date" value={t.date} error={errors[`trips.${i}.date`]} onChange={(v) => setTrip(i, { date: v })} />
                <TextField id={`${id}-${i}-purpose`} label="Purpose" value={t.purpose} max={500} onChange={(v) => setTrip(i, { purpose: v })} />
                <PlaceField id={`${id}-${i}-from`} label="From" value={t.from} places={places.data ?? []} error={errors[`trips.${i}.from`]} onChange={(v) => setTrip(i, { from: v })} />
                <PlaceField id={`${id}-${i}-to`} label="To" value={t.to} places={places.data ?? []} error={errors[`trips.${i}.to`]} onChange={(v) => setTrip(i, { to: v })} />
                <TextField
                  id={`${id}-${i}-distance`}
                  label="Distance driven, the whole trip (km)"
                  value={t.distance}
                  numeric
                  help="Read it from a map app: for a return trip, there and back."
                  error={errors[`trips.${i}.distance`]}
                  onChange={(v) => setTrip(i, { distance: v })}
                />
                <label className="fy-row gap-2 self-end">
                  <input type="checkbox" checked={t.return} onChange={(e) => setTrip(i, { return: e.target.checked })} />
                  A return trip
                </label>
              </div>
              <fieldset className="fy-stack fy-stack--2">
                <legend className="fy-label">Vehicle</legend>
                <div className="fy-row fy-row--wrap gap-3">
                  {(
                    [
                      ["car", "Car", Car],
                      ["motorcycle", "Motorcycle", Bike],
                    ] as const
                  ).map(([v, label, icon]) => (
                    <label key={v} className="fy-row gap-2">
                      <input type="radio" name={`${id}-${i}-vehicle`} checked={t.vehicle === v} onChange={() => setTrip(i, { vehicle: v })} />
                      <Icon of={icon} />
                      {label}
                    </label>
                  ))}
                </div>
              </fieldset>
              {trips.length > 1 && (
                <Button variant="ghost" size="sm" className="self-start" onClick={() => setTrips(trips.filter((_, j) => j !== i))} aria-label={`Remove trip ${i + 1}`}>
                  <Icon of={Trash2} />
                  Remove this trip
                </Button>
              )}
            </fieldset>
          ))}
          {errors.trips && <p className="fy-error">{errors.trips}</p>}
          <Button variant="ghost" className="self-start" onClick={() => setTrips([...trips, newTrip(trips[trips.length - 1]?.date ?? today)])}>
            <Icon of={Plus} />
            Add a trip
          </Button>
          <p className="fy-small fy-secondary">The distance is typed in: Fyniq sends no address anywhere. The amount is the distance, to 0.1 km, times the company's rate for the vehicle on the trip's date.</p>
          <div className="fy-row fy-row--wrap gap-2">
            <Button type="submit" disabled={busy} title={save_.tooltip}>
              <Icon of={Save} />
              {save_.label}
            </Button>
          </div>
        </form>
      )}
      <SavedPlaces places={places.data ?? []} />
    </div>
  );
}

function TextField({ id, label, value, onChange, error, help, type = "text", numeric, max }: { id: string; label: string; value: string; onChange: (v: string) => void; error?: string; help?: string; type?: string; numeric?: boolean; max?: number }) {
  const describedBy = [help && `${id}-help`, error && `${id}-error`].filter(Boolean).join(" ") || undefined;
  return (
    <div className="fy-field">
      <label className="fy-label" htmlFor={id}>
        {label}
      </label>
      <input className={numeric ? "fy-input fy-num" : "fy-input"} id={id} type={type} inputMode={numeric ? "decimal" : undefined} value={value} maxLength={max} onChange={(e) => onChange(e.target.value)} aria-invalid={error ? true : undefined} aria-describedby={describedBy} />
      {help && (
        <span className="fy-help" id={`${id}-help`}>
          {help}
        </span>
      )}
      {error && (
        <span className="fy-error" id={`${id}-error`}>
          {error}
        </span>
      )}
    </div>
  );
}

// PlaceField is a place typed, or picked from the member's saved places by name.
function PlaceField({ id, label, value, places, error, onChange }: { id: string; label: string; value: string; places: Place[]; error?: string; onChange: (v: string) => void }) {
  return (
    <div className="fy-stack fy-stack--2">
      <TextField id={id} label={label} value={value} max={300} error={error} onChange={onChange} />
      {places.length > 0 && (
        <div className="fy-chipset" role="group" aria-label={`Saved places for ${label}`}>
          {places.map((p) => (
            <button key={p.id} className="fy-filter" type="button" aria-pressed={value === p.address} onClick={() => onChange(p.address)}>
              <Icon of={MapPin} />
              {p.name}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// SavedPlaces saves a place the member drives between often, by name and address: their own only.
function SavedPlaces({ places }: { places: Place[] }) {
  const id = useId();
  const queries = useQueryClient();
  const [name, setName] = useState("");
  const [address, setAddress] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [said, setSaid] = useState("");
  const add = async () => {
    setProblem(null);
    const out = await api.POST("/v1/claim-places", { params: { header: { "Idempotency-Key": newKey() } }, body: { name: name.trim(), address: address.trim() } });
    if (!out.data) return setProblem(problemOf(out.error));
    setName("");
    setAddress("");
    setSaid(`Saved ${out.data.name}.`);
    await queries.invalidateQueries({ queryKey: ["claim-places"] });
  };
  return (
    <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-places`}>
      <h2 className="fy-h3" id={`${id}-places`}>
        Your saved places
      </h2>
      <p className="fy-small fy-muted">{places.length > 0 ? places.map((p) => p.name).join(", ") : "None yet. Save the places you drive between often, such as the office."}</p>
      {problem && <Refusal problem={problem} title="The place was not saved" />}
      {said && (
        <p className="fy-small" role="status">
          {said}
        </p>
      )}
      <div className="fy-grid fy-grid--2">
        <TextField id={`${id}-name`} label="Name" value={name} max={80} onChange={setName} />
        <TextField id={`${id}-address`} label="Address" value={address} max={300} onChange={setAddress} />
      </div>
      <Button variant="secondary" className="self-start" onClick={() => void add()} disabled={!name.trim() || !address.trim()}>
        <Icon of={Plus} />
        Save the place
      </Button>
    </section>
  );
}
