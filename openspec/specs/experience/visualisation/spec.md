# experience/visualisation Specification

## Purpose
How Fyniq shows numbers as charts, stat tiles and dashboards, following the project's data-visualisation method: the form chosen by the data's job, one y-axis per chart, stable and validated colours, reserved status colours, tooltips, legends and table views, recessive axes and configurable dashboard tiles. Derived from PRD-001 §7.5.

## Requirements

### Requirement: FR-VIZ-01 Form follows the data's job
The system SHALL choose each chart's form by the data's job: a **stat tile** for a single headline, lines for change over time, bars for comparison and a waterfall for bridges.

Source: PRD-001 §7.5 · Priority M · Phase P1

#### Scenario: Single headline
- **WHEN** a single headline figure such as cash, runway or burn is shown
- **THEN** it is a stat tile

#### Scenario: Change over time and comparison
- **WHEN** a figure is shown over time, or several figures are compared
- **THEN** change over time is a line chart and a comparison is a bar chart

#### Scenario: Bridges
- **WHEN** a bridge such as a cash bridge or a budget variance is shown
- **THEN** it is a waterfall chart

### Requirement: FR-VIZ-02 One y-axis per chart
Every chart SHALL have **one y-axis**.

Source: PRD-001 §7.5 · Priority M · Phase P1

#### Scenario: Two measures of different scale
- **WHEN** two measures of different scale are shown together
- **THEN** they become two charts or small multiples, never one chart with two y-axes

### Requirement: FR-VIZ-03 Stable categorical colours
Categorical colours SHALL be assigned in a **fixed order and follow the entity, not its rank**, and more than 8 series SHALL fold into "Other" or small multiples.

Source: PRD-001 §7.5 · Priority M · Phase P1

#### Scenario: Filter keeps colours
- **GIVEN** a chart with several series
- **WHEN** a filter removes one series
- **THEN** the remaining series keep their colours

#### Scenario: More than 8 series
- **WHEN** a chart would have more than 8 series
- **THEN** the extra series fold into "Other" or the chart becomes small multiples

### Requirement: FR-VIZ-04 Sequential and diverging scales
Sequential colour scales SHALL use one hue from light to dark, and diverging scales SHALL use two poles with a neutral grey midpoint.

Source: PRD-001 §7.5 · Priority M · Phase P1

#### Scenario: Sequential scale
- **WHEN** a sequential measure is coloured
- **THEN** the scale runs from light to dark in one hue

#### Scenario: Diverging scale
- **WHEN** a diverging measure such as a variance is coloured
- **THEN** the scale has two poles and a neutral grey midpoint

### Requirement: FR-VIZ-05 Reserved status colours
**Status colours SHALL be reserved** (good, warning, serious, critical) and always paired with an icon and a label.

Source: PRD-001 §7.5 · Priority M · Phase P1

#### Scenario: Status shown with icon and label
- **WHEN** a chart or tile shows a status
- **THEN** its status colour is paired with an icon and a label

#### Scenario: Status colours not used for data series
- **WHEN** a data series is coloured
- **THEN** it does not use a status colour

### Requirement: FR-VIZ-06 Tooltips, legends, table view and export
Every chart SHALL have hover tooltips, a legend when it has 2+ series, and a **table view** toggle, and SHALL export to PNG and its data to XLSX/CSV.

Source: PRD-001 §7.5 · Priority M · Phase P1

#### Scenario: Tooltips
- **WHEN** the user hovers over a data point
- **THEN** a tooltip shows its value

#### Scenario: Legend and direct labels
- **WHEN** a chart has 2 or more series
- **THEN** it has a legend
- **AND** with at most 4 series it also labels the series directly

#### Scenario: Table view
- **WHEN** the user toggles table view
- **THEN** the chart's data is shown as a table

#### Scenario: Export
- **WHEN** the user exports a chart
- **THEN** the chart exports to PNG and its data to XLSX or CSV

### Requirement: FR-VIZ-07 Palette validated in CI
The categorical palette SHALL be **validated by script** in CI for colour-vision-deficiency separation and contrast against the light and dark surfaces, and a palette change that fails validation SHALL fail the build.

Source: PRD-001 §7.5 · Priority M · Phase P1

#### Scenario: Failing palette change
- **WHEN** a palette change fails colour-vision-deficiency separation or contrast against the light or dark surface
- **THEN** the build fails

#### Scenario: Starting palette
- **WHEN** the categorical palette is first set
- **THEN** it is the validated reference set (blue, orange, aqua, yellow, magenta, green, violet, red) with separate light and dark steps
- **AND** the brand accent is not a data series colour

### Requirement: FR-VIZ-08 Text and recessive axes
Chart text SHALL use text colours, never the series colour, and gridlines and axes SHALL be recessive.

Source: PRD-001 §7.5 · Priority M · Phase P1

#### Scenario: Labels in text colours
- **WHEN** a chart shows labels, values or titles
- **THEN** they use text colours, not the colour of their series

#### Scenario: Recessive gridlines and axes
- **WHEN** a chart is drawn
- **THEN** its gridlines and axes are visually recessive behind the data

### Requirement: FR-VIZ-09 Configurable dashboards
Dashboards SHALL be composed of tiles a user can add, remove, resize and save per role, and tiles SHALL carry a "last updated" time and drill down (FR-UX-04).

Source: PRD-001 §7.5 · Priority S · Phase P2

#### Scenario: Arranging tiles
- **WHEN** a user adds, removes or resizes tiles and saves the dashboard
- **THEN** the layout is saved for the role

#### Scenario: Tile freshness and drill-down
- **WHEN** a tile is shown
- **THEN** it shows when it was last updated
- **AND** opening its figure drills down (FR-UX-04)

### Requirement: Starting categorical palette
Charts SHALL start from the validated reference palette (blue, orange, aqua, yellow, magenta, green, violet, red) with separate light and dark steps, and the brand accent SHALL NOT be a data-series colour.

Source: PRD-001 §7.5

#### Scenario: Palette order and themes
- **WHEN** a chart assigns categorical colours
- **THEN** it uses blue, orange, aqua, yellow, magenta, green, violet, red in that order, with the light or dark step of the current theme

#### Scenario: Brand accent never a series
- **WHEN** a chart colours its series
- **THEN** none of them uses the brand accent

#### Scenario: The violet series is not AI content
- **WHEN** a chart has seven or more series and shows the violet one
- **THEN** it uses the `--chart-*` violet, never the `--ai` tokens
- **AND** AI content is identified by the "AI" badge and its surfaces, never by hue alone (FR-UX-09), so an AI-drafted chart or commentary carries the badge
