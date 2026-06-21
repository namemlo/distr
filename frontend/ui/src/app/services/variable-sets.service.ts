import {HttpClient} from '@angular/common/http';
import {inject, Injectable} from '@angular/core';
import {Observable} from 'rxjs';
import {
  CreateUpdateVariableSetRequest,
  ResolvedVariable,
  ResolveVariablesPreviewRequest,
  VariableSet,
} from '../types/variable-set';

const baseUrl = '/api/v1/variable-sets';

@Injectable({
  providedIn: 'root',
})
export class VariableSetsService {
  private readonly httpClient = inject(HttpClient);

  list(): Observable<VariableSet[]> {
    return this.httpClient.get<VariableSet[]>(baseUrl);
  }

  get(id: string): Observable<VariableSet> {
    return this.httpClient.get<VariableSet>(`${baseUrl}/${id}`);
  }

  create(request: CreateUpdateVariableSetRequest): Observable<VariableSet> {
    return this.httpClient.post<VariableSet>(baseUrl, request);
  }

  update(id: string, request: CreateUpdateVariableSetRequest): Observable<VariableSet> {
    return this.httpClient.put<VariableSet>(`${baseUrl}/${id}`, request);
  }

  delete(id: string): Observable<void> {
    return this.httpClient.delete<void>(`${baseUrl}/${id}`);
  }

  resolvePreview(request: ResolveVariablesPreviewRequest): Observable<ResolvedVariable[]> {
    return this.httpClient.post<ResolvedVariable[]>('/api/v1/variables/resolve-preview', request);
  }
}
