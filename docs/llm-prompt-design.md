# LLM Prompt Design

## Strategy

DriftCal makes **one Claude API call per day** containing all of tomorrow's free gaps. This batch approach lets the model reason about the full day holistically — it won't suggest two high-energy activities back-to-back or repeat similar categories in adjacent gaps.

## Model Configuration

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| Model | `claude-sonnet-4-6` | Good balance of quality and cost for structured output |
| Temperature | 0.9 | High creativity for varied suggestions |
| Max tokens | 2000 | Sufficient for 3-5 structured suggestions |
| Response format | **Tool use (structured output)** | Schema-enforced JSON via `suggest_activities` tool definition |

### Why Tool Use Over Raw JSON Prompting

The suggestion engine uses Claude's **tool use / structured output** feature with a defined `suggest_activities` schema rather than asking for raw JSON in the prompt. This nearly eliminates JSON parsing failures, which are the primary failure mode of unstructured LLM output.

```json
{
  "name": "suggest_activities",
  "description": "Generate activity suggestions for free calendar gaps",
  "input_schema": {
    "type": "object",
    "properties": {
      "suggestions": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "gap_number": { "type": "integer" },
            "title": { "type": "string" },
            "description": { "type": "string" },
            "category": { "type": "string", "enum": ["outdoor", "cultural", "social", "fitness", "creative", "culinary", "relaxation"] },
            "energy_level": { "type": "string", "enum": ["low", "medium", "high"] },
            "estimated_cost": { "type": "string", "enum": ["free", "$", "$$", "$$$"] },
            "location": { "type": "string" },
            "location_url": { "type": ["string", "null"] },
            "reasoning": { "type": "string" }
          },
          "required": ["gap_number", "title", "description", "category", "energy_level", "estimated_cost", "location", "reasoning"]
        }
      }
    },
    "required": ["suggestions"]
  }
}
```

## System Prompt

```
You are DriftCal, a personal lifestyle assistant that suggests activities to fill
free time in someone's calendar. Your suggestions should be:

- **Specific and actionable** — not "go for a walk" but "Walk the Capital Crescent
  Trail from Bethesda to Georgetown (3.5 miles, mostly flat)"
- **Context-aware** — consider weather, time of day, energy level, what's before/after
- **Varied** — mix categories (outdoor, cultural, creative, social, culinary, fitness,
  relaxation) across the day and across days
- **Realistic** — account for travel time, prep time, and wind-down between activities
- **Encouraging but not pushy** — frame suggestions as invitations, not obligations

Never suggest the same activity twice within 14 days. Vary locations, categories,
and energy levels. If the user has rejected a category recently, reduce its frequency
but don't eliminate it entirely.

Respond with a JSON array of suggestions. Each suggestion must include all required
fields. Do not include any text outside the JSON.
```

## User Prompt Template

```
Generate activity suggestions for the free gaps in my schedule tomorrow.

## My Profile
- Location: {city}, {state}
- Interests (ranked): {interests}
- Anti-interests (never suggest): {anti_interests}
- Energy profile: Morning={morning_energy}, Afternoon={afternoon_energy}, Evening={evening_energy}
- Budget comfort: {budget_preference}
- Solo/social preference: {solo_social_ratio} (0=all social, 1=all solo)

## Tomorrow's Context
- Date: {date} ({day_of_week})
- Weather: {weather_summary}
  - High: {temp_high}°F, Low: {temp_low}°F
  - Conditions: {conditions}
  - Precipitation: {precip_chance}%
  - Sunrise: {sunrise}, Sunset: {sunset}

## My Schedule Tomorrow
{for each event}
- {start_time}-{end_time}: {title} ({category}) at {location}
{end for}

## Recurring Goals Already Scheduled Tomorrow
{for each scheduled goal instance}
- {start_time}-{end_time}: {goal_label} ({category}, {energy_level} energy)
{end for}
Note: These are already on the calendar. Suggest activities that complement them —
don't duplicate the category unless the gaps are far apart.

## Free Gaps (suggest activities for these — AFTER goals are placed)
{for each remaining gap}
- Gap {n}: {start_time}-{end_time} ({duration_minutes} min)
  - Time of day: {time_of_day}
  - Before: {before_event_title}
  - After: {after_event_title}
{end for}

## Recent Suggestion History (last 14 days, max 30 items)
{for each recent suggestion, capped at 30}
- {date}: "{title}" ({category}) — {status}
{end for}

## Local Events Happening Tomorrow
{for each local event, if available}
- {event_name} at {venue} ({time}) — {description}
{end for}

## Response Format
Use the suggest_activities tool to return your suggestions. One suggestion per gap.
```

## Example LLM Response

```json
[
  {
    "gap_number": 1,
    "title": "Walk to Dupont Farmers Market",
    "description": "It's 52°F and sunny — perfect walking weather. The Dupont Circle FreshFarm Market has winter root vegetables and a new hot cider vendor. Pick up something interesting for dinner tonight.",
    "category": "outdoor",
    "energy_level": "medium",
    "estimated_cost": "$",
    "location": "Dupont Circle FreshFarm Market",
    "location_url": "https://maps.google.com/?q=Dupont+Circle+FreshFarm+Market",
    "reasoning": "You haven't been outdoors in 3 days, the weather is ideal, and it aligns with your cooking interest."
  },
  {
    "gap_number": 2,
    "title": "Sketch session at the National Gallery",
    "description": "The new Impressionism exhibition just opened in the West Building. Bring a sketchbook and spend an hour drawing — the gallery provides folding stools. Focus on the Monet water lilies room.",
    "category": "cultural",
    "energy_level": "low",
    "estimated_cost": "free",
    "location": "National Gallery of Art, West Building",
    "location_url": "https://maps.google.com/?q=National+Gallery+of+Art",
    "reasoning": "Post-meeting low energy window. Creative + cultural combo you haven't done recently."
  },
  {
    "gap_number": 3,
    "title": "New tea blend + documentary night",
    "description": "Low energy evening at home. That pu-erh sampler you bought is still unopened. Brew a pot and watch 'Free Solo' — it's been on your list and pairs well with a cozy evening in.",
    "category": "relaxation",
    "energy_level": "low",
    "estimated_cost": "free",
    "location": "Home",
    "location_url": null,
    "reasoning": "End-of-day wind down. You've had 2 high-energy days — tonight should be restorative."
  }
]
```

## Feedback Loop

The `Recent Suggestion History` section in the prompt creates a natural feedback loop:

- **Approved suggestions** signal: "more like this"
- **Rejected suggestions** signal: "less like this"
- **Expired suggestions** (no action taken) are neutral

Over time, the LLM sees patterns in what gets approved vs rejected and adjusts. No fine-tuning or embeddings needed — it's all in-context learning.

## Cost Estimation

| Component | Per Call | Daily | Monthly |
|-----------|---------|-------|---------|
| Input tokens (~2000 tokens) | ~$0.006 | $0.006 | $0.18 |
| Output tokens (~800 tokens) | ~$0.008 | $0.008 | $0.24 |
| **Total** | **~$0.014** | **$0.014** | **~$0.42** |

Using Sonnet keeps costs under $1/month even with occasional manual re-generation requests.

## Fallback Behavior

| Scenario | Behavior |
|----------|----------|
| Claude API down | Skip suggestions for the day, send Telegram message: "Couldn't generate suggestions today. API issue." |
| Malformed JSON response | Tool use schema enforcement prevents most cases. Retry once with lower temperature (0.5). If still fails, skip. |
| No free gaps found | Don't call the API. Send Telegram: "Tomorrow's packed! No gaps to fill." |
| All gaps < 45 min | Don't call the API. Optionally note: "Only short breaks tomorrow — enjoy the hustle." |

## Future Enhancements (Phase 2+)

- **Conversation mode**: Reply to a suggestion in Telegram to negotiate ("Make it shorter", "Something indoors instead") — triggers a follow-up Claude call with the original suggestion + user feedback
- **Weekly themes**: "This week: try one new restaurant, one outdoor activity, one creative session"
- **Habit nudges**: "You haven't done anything outdoors in 5 days" injected into the system prompt
- **Cost tracking**: Running total of estimated spend on suggested activities
