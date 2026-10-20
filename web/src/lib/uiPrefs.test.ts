// =============================================================================
//  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
//
//  CodeDistill
//
//  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
//  Public License v3.0 (see the LICENSE file) and, separately, a commercial
//  license available from Nyx Software, Inc. Use outside the terms of one of those
//  licenses is prohibited.
//
//  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
// =============================================================================

import { describe, expect, it } from 'vitest';
import { uiPrefs, orderProjects } from './uiPrefs.svelte';

// The first front-end tests in this repo, written because one small preference
// took three attempts to make work and nothing here could have caught either
// failure: an async race, then a rune with a type argument that compiled to an
// ordinary function call. Both type-checked clean.

describe('orderProjects', () => {
  const projects = [
    { id: '1', name: 'Zebra' },
    { id: '2', name: 'alpha' },
    { id: '3', name: 'Project 10' },
    { id: '4', name: 'Project 9' },
  ];

  it('leaves creation order alone when the preference is off', () => {
    uiPrefs.sortProjectsAlphabetically = false;
    expect(orderProjects(projects).map((p) => p.name))
      .toEqual(['Zebra', 'alpha', 'Project 10', 'Project 9']);
  });

  it('sorts case-insensitively when on', () => {
    uiPrefs.sortProjectsAlphabetically = true;
    // Not ['Project 10', 'Project 9', 'Zebra', 'alpha'] — a naive sort puts
    // every capital before every lowercase, so "alpha" would land last.
    expect(orderProjects(projects).map((p) => p.name))
      .toEqual(['alpha', 'Project 9', 'Project 10', 'Zebra']);
  });

  it('sorts numbers the way people read them', () => {
    uiPrefs.sortProjectsAlphabetically = true;
    const names = orderProjects(projects).map((p) => p.name);
    expect(names.indexOf('Project 9')).toBeLessThan(names.indexOf('Project 10'));
  });

  it('does not mutate the array it was given', () => {
    uiPrefs.sortProjectsAlphabetically = true;
    const input = [...projects];
    orderProjects(input);
    // These arrays are the live project list several components render from;
    // Array.prototype.sort would reorder them in place underneath those views.
    expect(input.map((p) => p.name))
      .toEqual(['Zebra', 'alpha', 'Project 10', 'Project 9']);
  });

  it('reads the preference every call, not once', () => {
    // THE regression that cost three rounds. A value captured at module load —
    // or at component init — is read before its async load completes and never
    // read again.
    uiPrefs.sortProjectsAlphabetically = false;
    const unsorted = orderProjects(projects).map((p) => p.name);
    uiPrefs.sortProjectsAlphabetically = true;
    const sorted = orderProjects(projects).map((p) => p.name);
    expect(unsorted).not.toEqual(sorted);
  });
});
