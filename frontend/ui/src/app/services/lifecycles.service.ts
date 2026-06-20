import {HttpClient} from '@angular/common/http';
import {inject, Injectable} from '@angular/core';
import {Observable} from 'rxjs';
import {CreateUpdateLifecyclePhaseRequest, CreateUpdateLifecycleRequest, Lifecycle} from '../types/lifecycle';

const baseUrl = '/api/v1/lifecycles';

@Injectable({
  providedIn: 'root',
})
export class LifecyclesService {
  private readonly httpClient = inject(HttpClient);

  list(): Observable<Lifecycle[]> {
    return this.httpClient.get<Lifecycle[]>(baseUrl);
  }

  get(id: string): Observable<Lifecycle> {
    return this.httpClient.get<Lifecycle>(`${baseUrl}/${id}`);
  }

  create(request: CreateUpdateLifecycleRequest): Observable<Lifecycle> {
    return this.httpClient.post<Lifecycle>(baseUrl, request);
  }

  update(id: string, request: CreateUpdateLifecycleRequest): Observable<Lifecycle> {
    return this.httpClient.put<Lifecycle>(`${baseUrl}/${id}`, request);
  }

  replacePhases(id: string, phases: CreateUpdateLifecyclePhaseRequest[]): Observable<Lifecycle> {
    return this.httpClient.put<Lifecycle>(`${baseUrl}/${id}/phases`, {phases});
  }

  delete(id: string): Observable<void> {
    return this.httpClient.delete<void>(`${baseUrl}/${id}`);
  }
}
